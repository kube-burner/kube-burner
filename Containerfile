FROM golang:1.20 AS builder

WORKDIR /go/src/github.com/cloud-bulldozer/kube-burner/

ADD ./ /go/src/github.com/cloud-bulldozer/kube-burner/

RUN CGO_ENABLED=0 go build \
  -tags netgo \
  -v \
  -a \
  -ldflags '-extldflags "-static"' \
  -o bin/kube-burner ./cmd/kube-burner

FROM bash:5-alpine3.18 as distro
RUN apk add rsync

COPY --from=builder /go/src/github.com/cloud-bulldozer/kube-burner/bin/kube-burner /bin/kube-burner

LABEL io.k8s.display-name="kube-burner" \
      maintainer="Raul Sevilla <rsevilla@redhat.com"
ENTRYPOINT ["/bin/kube-burner"]
