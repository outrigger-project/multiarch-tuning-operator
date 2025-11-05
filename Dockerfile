# The base image is expected to contain
# /bin/opm (with a serve subcommand) and /bin/grpc_health_probe
ARG BASE_IMAGE=${BASE_IMAGE:-registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.19}
FROM ${BASE_IMAGE}
ARG OCP_VERSION=${OCP_VERSION:-fbc-v4-19}

# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]

ADD licenses/ /licenses/
# Copy declarative config root into image at /configs and pre-populate serve cache
ADD ${OCP_VERSION}/catalog/ /configs
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

# Set DC-specific label for the location of the DC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs
