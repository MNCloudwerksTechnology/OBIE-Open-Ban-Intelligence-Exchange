# syntax=docker/dockerfile:1
#
# OBIE website container image (ADR 0018): the prerendered Angular front end
# inside the Spring Boot jar, on a JRE 21 as the unprivileged user obie
# (10001). Build context is this directory (website/).
#
#   docker build -t obie-website .
#   docker run --read-only --tmpfs /tmp -m 512m -p 8080:8080 --env-file … obie-website
#
# deploy/compose.yaml runs it with PostgreSQL; deploy/README.md describes the
# deployment and every environment variable is in deploy/.env.example.
#
# The front end and the jar are platform-independent, so both build stages
# run on the build platform; only the runtime stage is per target platform.
# The build skips the tests: `make -C website ci` runs them.

ARG NODE_VERSION=24
ARG JAVA_VERSION=21

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend ./
RUN npm run build

FROM --platform=$BUILDPLATFORM eclipse-temurin:${JAVA_VERSION}-jdk-alpine AS backend
WORKDIR /src/backend
COPY backend ./
COPY --from=frontend /src/frontend/dist/frontend/browser /src/frontend/dist/frontend/browser
RUN --mount=type=cache,target=/root/.m2 \
    ./mvnw -B -q package -Dmaven.test.skip=true && \
    java -Djarmode=tools -jar target/obie-website.jar extract --layers --launcher --destination /out

FROM eclipse-temurin:${JAVA_VERSION}-jre-alpine
ARG VERSION=dev
LABEL org.opencontainers.image.title="OBIE website" \
      org.opencontainers.image.description="The public website of OBIE (Open Ban Intelligence Exchange)" \
      org.opencontainers.image.source="https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$VERSION"
RUN addgroup -S -g 10001 obie && adduser -S -D -H -u 10001 -G obie -s /sbin/nologin obie
WORKDIR /app
# Least-changing layer first; everything stays owned by root and read-only.
COPY --from=backend /out/dependencies/ ./
COPY --from=backend /out/spring-boot-loader/ ./
COPY --from=backend /out/snapshot-dependencies/ ./
COPY --from=backend /out/application/ ./
USER 10001:10001
# Sized for a 512 MB container: at most half of it for the heap, capped
# metaspace, code cache and thread stacks, the serial collector (no GC
# threads), and a clean exit instead of a limping JVM on OutOfMemoryError.
# The application writes nothing but Tomcat's work files in /tmp, so the
# root filesystem can be read-only with a tmpfs on /tmp; -XX:-UsePerfData
# keeps the JVM from writing to /tmp as well. Replace the variable to tune.
ENV JDK_JAVA_OPTIONS="-XX:MaxRAMPercentage=50 -XX:InitialRAMPercentage=12.5 -XX:MaxMetaspaceSize=160m -XX:ReservedCodeCacheSize=64m -XX:MaxDirectMemorySize=32m -Xss512k -XX:+UseSerialGC -XX:+ExitOnOutOfMemoryError -XX:-UsePerfData" \
    SERVER_PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${SERVER_PORT}/api/health" || exit 1
ENTRYPOINT ["java", "org.springframework.boot.loader.launch.JarLauncher"]
