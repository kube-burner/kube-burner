FROM registry.access.redhat.com/ubi8:latest as builder

COPY . /root/kube-burner
RUN dnf install -y golang make
RUN make clean -C /root/kube-burner && make build -C /root/kube-burner

FROM registry.access.redhat.com/ubi8:latest

COPY --from=builder /root/kube-burner/bin/kube-burner /bin/kube-burner
LABEL maintainer="Raul Sevilla <rsevilla@redhat.com"
WORKDIR /root
ENTRYPOINT /bin/kube-burner
