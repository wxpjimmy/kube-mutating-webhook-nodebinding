# Kubernetes Mutating Webhook for Nodebinding

This tutoral shows how to build and deploy a [MutatingAdmissionWebhook](https://kubernetes.io/docs/admin/admission-controllers/#mutatingadmissionwebhook-beta-in-19) that bind pod to dedicated node.

## Prerequisites

- [git](https://git-scm.com/downloads)
- [go](https://golang.org/dl/) version v1.12+
- [docker](https://docs.docker.com/install/) version 17.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster with the `admissionregistration.k8s.io/v1beta1` API enabled. Verify that by the following command:

```
kubectl api-versions | grep admissionregistration.k8s.io
```
The result should be:
```
admissionregistration.k8s.io/v1
admissionregistration.k8s.io/v1beta1
```

> Note: In addition, the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook` admission controllers should be added and listed in the correct order in the admission-control flag of kube-apiserver.

## Build

1. Build binary

```
# make build
```

2. Build docker image
   
```
# make build-image
```

3. push docker image

```
# make push-image
```

> Note: log into the docker registry before pushing the image.

## Deploy

1. Create namespace `nodebinding` in which the nodebinding webhook is deployed:

```
# kubectl create ns nodebinding
```

2. Create a signed cert/key pair and store it in a Kubernetes `secret` that will be consumed by sidecar injector deployment:

```
# ./deploy/webhook-create-signed-cert.sh \
    --service nodebinding-webhook-svc \
    --secret nodebinding-webhook-certs \
    --namespace nodebinding
```

3. Patch the `MutatingWebhookConfiguration` by set `caBundle` with correct value from Kubernetes cluster:

```
# cat deploy/mutatingwebhook.yaml | \
    deploy/webhook-patch-ca-bundle.sh > \
    deploy/mutatingwebhook-ca-bundle.yaml
```

4. Deploy resources:

```
# kubectl create -f deploy/deployment.yaml
# kubectl create -f deploy/service.yaml
# kubectl create -f deploy/mutatingwebhook-ca-bundle.yaml
```

## Verify

1. The nodebinding webhook should be in running state:

```
# kubectl -n nodebinding get pod
NAME                                                   READY   STATUS    RESTARTS   AGE
nodebinding-webhook-deployment-7c8bc5f4c9-28c84   1/1     Running   0          30s
# kubectl -n nodebinding get deploy
NAME                                  READY   UP-TO-DATE   AVAILABLE   AGE
nodebinding-webhook-deployment   1/1     1            1           67s
```
2. label the namespace `nodebinding` with:

```
# kubectl label -n nodebinding kf-partition=nodebinding
```

3. create a nodepool with name `nodebinding` and attached the nodepool to the aks cluster
   
4. taint a node in agentpool `nodebinding` with `kubectl taint node xxxx tf-partition=nodebinding:NoExecute`.

5. Deploy an app in Kubernetes cluster, take `alpine` app as an example

```
# kubectl run alpine --image=alpine --restart=Never -n injection --command -- sleep infinity
```

6. Verify the pod is created in the tainted node in step3

## Troubleshooting

Sometimes you may find that pod is not scheduled as expected, check the following items:

1. The nodebinding webhook is in running state and no error logs.
2. The namespace in which application pod is deployed has the correct labels as configured in `mutatingwebhookconfiguration`.
3. Check the `caBundle` is patched to `mutatingwebhookconfiguration` object by checking if `caBundle` fields is empty.