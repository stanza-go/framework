package auth

import (
	nethttp "net/http"
	"time"
)

// Cookie names for access and refresh tokens.
const (
	AccessTokenCookie  = "access_token"
	RefreshTokenCookie = "refresh_token"
)

// SetAccessTokenCookie sets the JWT access token as an HttpOnly cookie.
// The cookie path is the configured base path (default "/api/admin").
func (a *Auth) SetAccessTokenCookie(w nethttp.ResponseWriter, token string) {
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     AccessTokenCookie,
		Value:    token,
		Path:     a.cookiePath,
		MaxAge:   int(a.accessTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: nethttp.SameSiteLaxMode,
	})
}

// SetRefreshTokenCookie sets the opaque refresh token as an HttpOnly
// cookie. The cookie path is restricted to auth endpoints (default
// "/api/admin/auth") to limit exposure.
func (a *Auth) SetRefreshTokenCookie(w nethttp.ResponseWriter, token string) {
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     RefreshTokenCookie,
		Value:    token,
		Path:     a.authCookiePath,
		MaxAge:   int(a.refreshTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: nethttp.SameSiteLaxMode,
	})
}

// ClearAccessTokenCookie removes the access token cookie by setting
// it to an empty value with an immediate expiry.
func (a *Auth) ClearAccessTokenCookie(w nethttp.ResponseWriter) {
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     AccessTokenCookie,
		Value:    "",
		Path:     a.cookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: nethttp.SameSiteLaxMode,
	})
}

// ClearRefreshTokenCookie removes the refresh token cookie by setting
// it to an empty value with an immediate expiry.
func (a *Auth) ClearRefreshTokenCookie(w nethttp.ResponseWriter) {
	nethttp.SetCookie(w, &nethttp.Cookie{
		Name:     RefreshTokenCookie,
		Value:    "",
		Path:     a.authCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: nethttp.SameSiteLaxMode,
	})
}

// ClearAllCookies removes both access and refresh token cookies.
func (a *Auth) ClearAllCookies(w nethttp.ResponseWriter) {
	a.ClearAccessTokenCookie(w)
	a.ClearRefreshTokenCookie(w)
}

// ReadAccessToken extracts the access token from the request cookie.
// Returns ErrNoToken if the cookie is not present.
func ReadAccessToken(r *nethttp.Request) (string, error) {
	c, err := r.Cookie(AccessTokenCookie)
	if err != nil {
		return "", ErrNoToken
	}
	return c.Value, nil
}

// ReadRefreshToken extracts the refresh token from the request cookie.
// Returns ErrNoToken if the cookie is not present.
func ReadRefreshToken(r *nethttp.Request) (string, error) {
	c, err := r.Cookie(RefreshTokenCookie)
	if err != nil {
		return "", ErrNoToken
	}
	return c.Value, nil
}
