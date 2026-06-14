package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// deviceCodeGrant is GitHub's OAuth device-flow grant type.
const deviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

type deviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// Login runs the GitHub device-code flow, printing the verification URL and
// user code to w, then polls until the user authorizes and returns the durable
// GitHub OAuth token. It blocks until authorization completes, ctx is
// cancelled, or the device code expires.
func Login(ctx context.Context, w io.Writer, opts ...Option) (string, error) {
	o := newOptions(opts...)

	dc, err := requestDeviceCode(ctx, o)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(w, "To authenticate, open %s and enter code: %s\n", dc.VerificationURI, dc.UserCode)

	return pollForToken(ctx, o, dc)
}

func requestDeviceCode(ctx context.Context, o options) (deviceCode, error) {
	form := url.Values{"client_id": {o.clientID}, "scope": {o.scope}}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoints.DeviceCode, strings.NewReader(form.Encode()))
	if err != nil {
		return deviceCode{}, fmt.Errorf("build device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return deviceCode{}, fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return deviceCode{}, fmt.Errorf("request device code: status %d", resp.StatusCode)
	}

	var dc deviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return deviceCode{}, fmt.Errorf("decode device code: %w", err)
	}
	if dc.DeviceCode == "" {
		return deviceCode{}, errors.New("device code response missing device_code")
	}
	return dc, nil
}

func pollForToken(ctx context.Context, o options, dc deviceCode) (string, error) {
	interval := time.Duration(dc.Interval+1) * time.Second
	if interval <= 0 {
		interval = 6 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}

		if dc.ExpiresIn > 0 && time.Now().After(deadline) {
			return "", errors.New("device authorization expired; rerun login")
		}

		tok, errCode, err := pollOnce(ctx, o, dc)
		if err != nil {
			return "", err
		}
		switch {
		case tok != "":
			return tok, nil
		case errCode == "authorization_pending":
			// keep polling at the current interval
		case errCode == "slow_down":
			interval += 5 * time.Second
		case errCode == "expired_token", errCode == "access_denied":
			return "", fmt.Errorf("device authorization rejected: %s", errCode)
		case errCode != "":
			return "", fmt.Errorf("device authorization returned %s", errCode)
		}
	}
}

// pollOnce performs a single access-token poll. It returns the token on
// success, otherwise the GitHub error code (e.g. authorization_pending).
func pollOnce(ctx context.Context, o options, dc deviceCode) (token, errCode string, err error) {
	form := url.Values{
		"client_id":   {o.clientID},
		"device_code": {dc.DeviceCode},
		"grant_type":  {deviceCodeGrant},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoints.AccessToken, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", fmt.Errorf("build access token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("poll access token: %w", err)
	}
	defer resp.Body.Close()

	var ar struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return "", "", fmt.Errorf("decode access token: %w", err)
	}
	return ar.AccessToken, ar.Error, nil
}
