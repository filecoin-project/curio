ARG CURIO_TEST_IMAGE=curio/curio-all-in-one:latest
#############################################################################
FROM ${CURIO_TEST_IMAGE}

ARG BUILD_VERSION=0.1

LABEL org.opencontainers.image.version=$BUILD_VERSION \
      org.opencontainers.image.authors="Curio Dev Team" \
      name="contracts-bootstrap-dev" \
      maintainer="Curio Dev Team" \
      vendor="Curio Dev Team" \
      version=$BUILD_VERSION \
      release=$BUILD_VERSION \
      summary="This image bootstraps devnet contracts" \
      description="This image is used to deploy and write contract addresses for Curio devnet"

WORKDIR /app
RUN mkdir -p /app

COPY entrypoint.sh /app/contracts-bootstrap-entrypoint.sh
RUN chmod +x /app/contracts-bootstrap-entrypoint.sh

USER root

ENTRYPOINT ["/app/contracts-bootstrap-entrypoint.sh"]
