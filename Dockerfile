FROM golang:alpine

ARG version
ARG proxy

# Download packages from aliyun mirrors
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk --update add --no-cache ca-certificates tzdata git openssh-client

COPY . /go/src/github.com/tengattack/minimetric
RUN cd /go/src/github.com/tengattack/minimetric \
  && http_proxy=$proxy https_proxy=$proxy go get -d -v ./... \
  && cd /go/src/k8s.io/klog && git checkout v1.0.0 \
  && cd - \
  && GOOS=linux CGO_ENABLED=0 go install -ldflags "-X main.Version=$version"

FROM scratch

COPY --from=0 /usr/share/zoneinfo /usr/share/zoneinfo
#COPY --from=0 /usr/share/ca-certificates /usr/share/ca-certificates
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=0 /etc/passwd /etc/
COPY --from=0 /go/bin/minimetric /bin/

WORKDIR /

USER nobody

ENTRYPOINT ["/bin/minimetric"]
