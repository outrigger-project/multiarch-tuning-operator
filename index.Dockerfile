# The base image is expected to contain
# /bin/opm (with a serve subcommand) and /bin/grpc_health_probe
FROM registry.redhat.io/openshift4/ose-operator-registry:v4.13 as builder
USER 0
COPY index.base.yaml /tmp/index.yaml
RUN opm render registry.ci.openshift.org/origin/multiarch-tuning-op-bundle:v1.x  --output=yaml >> /tmp/index.yaml
#RUN cat /tmp/index.yaml

FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.16

# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]

COPY LICENSE /licenses/license.txt
# Copy declarative config root into image at /configs and pre-populate serve cache
COPY --from=builder /tmp/index.yaml /configs/multiarch-tuning-operator/index.yaml

RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

# Set DC-specific label for the location of the DC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs
