package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
	"gomodules.xyz/jsonpatch/v2"
	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	// (https://github.com/kubernetes/kubernetes/issues/57982)
	defaulter = runtime.ObjectDefaulter(runtimeScheme)
)

var ignoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

const (
	admissionWebhookAnnotationStatusKey = "nodebinding-webhook.wxpjimmy.me/status"
)

type WebhookServer struct {
	server *http.Server
}

// Webhook Server parameters
type WhSvrParameters struct {
	port     int    // webhook server port
	certFile string // path to the x509 certificate for https
	keyFile  string // path to the x509 private key matching `CertFile`
}

// type patchOperation struct {
// 	Op    string      `json:"op"`
// 	Path  string      `json:"path"`
// 	Value interface{} `json:"value,omitempty"`
// }

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionregistrationv1beta1.AddToScheme(runtimeScheme)
	// defaulting with webhooks:
	// https://github.com/kubernetes/kubernetes/issues/57982
	//  _ = v1.AddToScheme(runtimeScheme)
}

// main mutation process
func (whsvr *WebhookServer) mutate(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		glog.Errorf("Could not unmarshal raw object: %v", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v Operation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, pod.Name, req.UID, req.Operation, req.UserInfo)

	newPod, err := whsvr.mutatePod(&pod, req.Namespace)
	if err != nil {
		glog.Errorf("Error while mutating oldPod: %v, Pod=%v, Namespace=%v", err, pod, req.Namespace)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	patchBytes, err := whsvr.getPatch(req.Object.Raw, newPod)
	if err != nil {
		glog.Errorf("Error while creating Patch: %v, oldPod=%v, newPod=%v, Namespace=%v", err, pod, newPod, req.Namespace)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	glog.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func (whsvr *WebhookServer) mutatePod(oldPod *corev1.Pod, namespace string) (*corev1.Pod, error) {
	if oldPod == nil {
		err := errors.New("oldPod can't be nil")
		return nil, err
	}
	if namespace == "" || namespace == "default" {
		err := errors.New("namespace can't be empty or 'default'")
		return nil, err
	}

	newPod := oldPod.DeepCopy()

	if newPod.Spec.NodeSelector == nil {
		newPod.Spec.NodeSelector = map[string]string{}
	}
	newPod.Spec.NodeSelector["agentpool"] = namespace

	partitionToleration := corev1.Toleration{
		Key:      "kf-partition",
		Operator: corev1.TolerationOpEqual,
		Value:    namespace,
		Effect:   corev1.TaintEffectNoExecute,
	}
	if newPod.Spec.Tolerations == nil {
		newPod.Spec.Tolerations = []corev1.Toleration{}
	}
	newPod.Spec.Tolerations = append(
		[]corev1.Toleration{partitionToleration},
		newPod.Spec.Tolerations...)

	if newPod.Annotations == nil {
		newPod.Annotations = map[string]string{}
	}
	newPod.Annotations[admissionWebhookAnnotationStatusKey] = "intercepted"

	return newPod, nil
}

func (whsvr *WebhookServer) getPatch(rawOldPod []byte, newPod *corev1.Pod) ([]byte, error) {
	if newPod == nil {
		err := errors.New("newPod is nil when trying to get patch for the old pod")
		glog.Errorf("newPod is nil, can't get patch： %v", err)
		return nil, err
	}

	rawNewPod, err := json.Marshal(newPod)
	if err != nil {
		glog.Errorf("Error while parsing new pod: %v, Pod=%v", err, newPod)
		return nil, err
	}

	patches, err := jsonpatch.CreatePatch(rawOldPod, rawNewPod)
	if err != nil {
		glog.Errorf(
			"Error while trying to get the needed patch: %v, rawOldPod: %v, rawNewPod: %v",
			err,
			string(rawOldPod),
			string(rawNewPod),
		)
		return nil, err
	}
	return json.Marshal(patches)
}

// Serve method for webhook server
func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = whsvr.mutate(&ar)
	}

	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("Ready to write reponse ...")
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}
