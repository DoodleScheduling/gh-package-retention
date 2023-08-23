FROM gcr.io/distroless/static:latest
WORKDIR /
COPY gh-package-retention gh-package-retention

ENTRYPOINT ["/gh-package-retention"]
