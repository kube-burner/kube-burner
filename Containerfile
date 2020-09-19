FROM registry.fedoraproject.org/fedora-minimal:latest as builder

RUN microdnf install golang make git
COPY . /root/kube-burner
RUN make clean -C /root/kube-burner && make build -C /root/kube-burner

FROM registry.fedoraproject.org/fedora-minimal:latest

COPY --from=builder /root/kube-burner/bin/kube-burner /bin/kube-burner
LABEL maintainer="Raul Sevilla <rsevilla@redhat.com"
ENTRYPOINT ["/bin/kube-burner"]
