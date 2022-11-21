FROM registry.fedoraproject.org/fedora-minimal:latest as builder
COPY . /kube-burner
RUN microdnf install -y golang make
RUN make -C kube-burner build

FROM registry.fedoraproject.org/fedora-minimal:latest
COPY --from=builder kube-burner/bin/kube-burner /bin/kube-burner
LABEL io.k8s.display-name="kube-burner" \
      maintainer="Raul Sevilla <rsevilla@redhat.com"
ENTRYPOINT ["/bin/kube-burner"]
