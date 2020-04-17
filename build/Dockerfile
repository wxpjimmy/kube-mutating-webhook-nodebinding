FROM alpine:latest

# set labels for metadata
LABEL maintainer="Xiaopeng<wxpjimmy@gmail.com>" \
  name="nodebinding" \
  description="A Kubernetes mutating webhook server that implements node binding" \
  summary="A Kubernetes mutating webhook server that implements node binding"

# set environment variables
ENV NODE_BINDING=/usr/local/bin/nodebinding \
  USER_UID=1001 \
  USER_NAME=nodebinding

# install nodebinding binary
COPY build/_output/bin/nodebinding ${NODE_BINDING}

# copy licenses
RUN mkdir /licenses
COPY LICENSE /licenses

# set entrypoint
ENTRYPOINT ["/usr/local/bin/nodebinding"]

# switch to non-root user
USER ${USER_UID}
