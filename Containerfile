FROM registry.fedoraproject.org/fedora-minimal:latest

ARG ARCH=amd64
COPY kube-burner /bin/kube-burner
LABEL maintainer="Raul Sevilla <rsevilla@redhat.com"
ENTRYPOINT ["/bin/kube-burner"]
