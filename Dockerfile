FROM quay.io/centos/centos:stream8

USER root

RUN dnf install -y centos-release-nfv-openvswitch

RUN dnf install --best --refresh -y \
        golang \
        openvswitch2.15 \
        make
RUN dnf clean all && rm -rf /var/cache/dnf/*

COPY . /src
WORKDIR /src

RUN make

RUN cp build/ovs-flowmon /usr/bin/ovs-flowmon
RUN cp dist/entrypoint.sh /root/entrypoint.sh

WORKDIR /root
RUN rm -rf /src

ENTRYPOINT /root/entrypoint.sh

