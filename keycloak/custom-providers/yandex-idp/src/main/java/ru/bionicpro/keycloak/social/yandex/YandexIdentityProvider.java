package ru.bionicpro.keycloak.social.yandex;

import com.fasterxml.jackson.databind.JsonNode;
import org.keycloak.broker.oidc.AbstractOAuth2IdentityProvider;
import org.keycloak.broker.oidc.OAuth2IdentityProviderConfig;
import org.keycloak.broker.oidc.mappers.AbstractJsonUserAttributeMapper;
import org.keycloak.broker.provider.BrokeredIdentityContext;
import org.keycloak.broker.provider.IdentityBrokerException;
import org.keycloak.broker.provider.util.SimpleHttp;
import org.keycloak.broker.social.SocialIdentityProvider;
import org.keycloak.events.EventBuilder;
import org.keycloak.models.KeycloakSession;

/**
 * Identity broker for Yandex ID.
 *
 * Yandex OAuth is plain OAuth 2.0, not OpenID Connect: it does not recognize
 * the "openid" scope and its userinfo endpoint returns Yandex-specific
 * fields (id, login, default_email, ...) instead of standard OIDC claims.
 * Keycloak's built-in "oidc" provider type unconditionally injects "openid"
 * into every authorization request (see OIDCIdentityProvider's
 * constructor), which Yandex rejects with invalid_scope. Extending
 * AbstractOAuth2IdentityProvider directly (the same base class the built-in
 * GitHub provider uses) avoids that and lets us control the scope and
 * profile parsing ourselves.
 */
public class YandexIdentityProvider extends AbstractOAuth2IdentityProvider<OAuth2IdentityProviderConfig>
        implements SocialIdentityProvider<OAuth2IdentityProviderConfig> {

    public static final String AUTH_URL = "https://oauth.yandex.ru/authorize";
    public static final String TOKEN_URL = "https://oauth.yandex.ru/token";
    public static final String PROFILE_URL = "https://login.yandex.ru/info";
    public static final String DEFAULT_SCOPE = "login:info login:email";

    public YandexIdentityProvider(KeycloakSession session, OAuth2IdentityProviderConfig config) {
        super(session, config);
        config.setAuthorizationUrl(AUTH_URL);
        config.setTokenUrl(TOKEN_URL);
        config.setUserInfoUrl(PROFILE_URL);
    }

    @Override
    protected String getDefaultScopes() {
        return DEFAULT_SCOPE;
    }

    @Override
    protected BrokeredIdentityContext doGetFederatedIdentity(String accessToken) {
        try {
            // Yandex expects the token as an "oauth_token" query parameter
            // (its documented alternative to the "Authorization: OAuth <token>"
            // header), which sidesteps any ambiguity with Keycloak's default
            // "Bearer" header handling.
            JsonNode profile = SimpleHttp.doGet(PROFILE_URL, session)
                    .param("format", "json")
                    .param("oauth_token", accessToken)
                    .asJson();
            return extractIdentityFromProfile(null, profile);
        } catch (Exception e) {
            throw new IdentityBrokerException("Could not obtain user profile from Yandex ID.", e);
        }
    }

    @Override
    protected BrokeredIdentityContext extractIdentityFromProfile(EventBuilder event, JsonNode profile) {
        String id = getJsonProperty(profile, "id");
        BrokeredIdentityContext user = new BrokeredIdentityContext(id);

        String login = getJsonProperty(profile, "login");
        user.setUsername(login != null ? login : id);
        user.setEmail(getJsonProperty(profile, "default_email"));
        user.setFirstName(getJsonProperty(profile, "first_name"));
        user.setLastName(getJsonProperty(profile, "last_name"));

        user.setIdpConfig(getConfig());
        user.setIdp(this);
        AbstractJsonUserAttributeMapper.storeUserProfileForMapper(user, profile, getConfig().getAlias());

        return user;
    }
}
