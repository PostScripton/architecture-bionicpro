# Builds a Keycloak image with a custom Yandex ID identity broker baked in.
#
# Keycloak's built-in "oidc" provider type unconditionally injects the
# "openid" scope into every authorization request (OIDCIdentityProvider's
# constructor does this unconditionally, regardless of config). Yandex OAuth
# is plain OAuth2, not OIDC, and rejects "openid" as an unknown scope
# (invalid_scope). The fix is a small custom identity provider extension
# (see custom-providers/yandex-idp) that extends AbstractOAuth2IdentityProvider
# directly - the same base class Keycloak's built-in GitHub provider uses -
# instead of the OIDC-specific subclass.
#
# The provider is compiled against Keycloak's own server jars (no Maven/
# internet dependency resolution needed beyond pulling this base image).

FROM quay.io/keycloak/keycloak:21.1 AS kc-libs

FROM eclipse-temurin:17-jdk AS build
COPY --from=kc-libs /opt/keycloak/lib/lib/main /kc-libs
COPY custom-providers/yandex-idp/src/main/java /src/java
COPY custom-providers/yandex-idp/src/main/resources /src/resources
RUN mkdir -p /build/classes \
    && javac -cp "/kc-libs/*" -d /build/classes $(find /src/java -name '*.java') \
    && cp -r /src/resources/* /build/classes/ \
    && cd /build/classes && jar cf /build/yandex-idp.jar .

FROM quay.io/keycloak/keycloak:21.1
COPY --from=build /build/yandex-idp.jar /opt/keycloak/providers/yandex-idp.jar
