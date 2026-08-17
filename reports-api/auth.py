import time

import httpx
from fastapi import HTTPException, Request
from jose import jwt
from jose.exceptions import JOSEError

import config

_jwks_cache: dict = {"keys": [], "fetched_at": 0.0}
_JWKS_TTL_SECONDS = 300


def _fetch_jwks() -> dict:
    now = time.time()
    if not _jwks_cache["keys"] or now - _jwks_cache["fetched_at"] > _JWKS_TTL_SECONDS:
        resp = httpx.get(config.JWKS_URL, timeout=5.0)
        resp.raise_for_status()
        _jwks_cache["keys"] = resp.json()["keys"]
        _jwks_cache["fetched_at"] = now
    return _jwks_cache["keys"]


def _find_key(kid: str) -> dict:
    for key in _fetch_jwks():
        if key.get("kid") == kid:
            return key
    # kid not found - Keycloak may have rotated keys, force a refresh once
    _jwks_cache["fetched_at"] = 0.0
    for key in _fetch_jwks():
        if key.get("kid") == kid:
            return key
    raise HTTPException(status_code=401, detail="unknown signing key")


class Principal:
    def __init__(self, username: str, roles: list[str]):
        self.username = username
        self.roles = roles

    def has_role(self, role: str) -> bool:
        return role in self.roles


def authenticate(request: Request) -> Principal:
    """Проверяет подпись, issuer и audience access-токена и возвращает
    личность вызывающего пользователя. Это единственный источник правды о
    том, кто делает запрос - параметры пути/запроса на это не влияют, что
    исключает доступ к чужим отчётам."""
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="missing bearer token")
    token = auth_header[len("Bearer "):]

    try:
        unverified_header = jwt.get_unverified_header(token)
        key = _find_key(unverified_header["kid"])
        claims = jwt.decode(
            token,
            key,
            algorithms=[key.get("alg", "RS256")],
            audience=config.REQUIRED_AUDIENCE,
            issuer=config.ISSUER,
        )
    except JOSEError as exc:
        raise HTTPException(status_code=401, detail=f"invalid token: {exc}") from exc
    except KeyError as exc:
        raise HTTPException(status_code=401, detail="malformed token") from exc

    username = claims.get("preferred_username")
    if not username:
        raise HTTPException(status_code=401, detail="token has no preferred_username")

    roles = claims.get("realm_access", {}).get("roles", [])
    return Principal(username=username, roles=roles)


def require_report_access(request: Request) -> Principal:
    principal = authenticate(request)
    if not principal.has_role(config.REQUIRED_ROLE):
        raise HTTPException(status_code=403, detail="user has no prosthetic reports available")
    return principal
