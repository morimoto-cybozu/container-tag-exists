package pkg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

var (
	authAPI     = "https://%s/v2/auth?service=%s&scope=repository:%s:pull"
	manifestAPI = "https://%s/v2/%s/manifests/%s"
)

type IRegistryClient interface {
	IsTagExist(tag string) (bool, error)
}

type RegistryClient struct {
	RegistryName string
	RegistryURL  string
	ImagePath    string
	HttpClient   *http.Client
}

type tokenResponse struct {
	Token string `json:"token"`
}

func (r RegistryClient) retrieve(method, endpoint, auth string) (int, []byte, error) {
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return -1, nil, err
	}
	req.Header.Add("Authorization", auth)
	res, err := r.HttpClient.Do(req)
	if err != nil {
		return -1, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return -1, nil, err
	}
	return res.StatusCode, b, nil
}

func (r RegistryClient) retrieveBearerToken(auth string) (string, error) {
	endpoint := fmt.Sprintf(authAPI, r.RegistryURL, r.RegistryURL, r.ImagePath)
	status, res, err := r.retrieve(http.MethodGet, endpoint, fmt.Sprintf("Basic %s", auth))
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("unexpected response code %d", status)
	}
	var token tokenResponse
	if err := json.Unmarshal(res, &token); err != nil {
		return "", err
	}
	return token.Token, nil
}

func (r RegistryClient) checkManifestForTag(bearer, tag string) (bool, error) {
	endpoint := fmt.Sprintf(manifestAPI, r.RegistryURL, r.ImagePath, tag)
	auth := ""
	if bearer != "" {
		auth = fmt.Sprintf("Bearer %s", bearer)
	}
	status, _, err := r.retrieve(http.MethodHead, endpoint, auth)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK {
		return true, nil
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("unexpected response registry API: %d", status)
}

func (r RegistryClient) getAuthTokenFromCredentials() (string, error) {
	userEnvName := fmt.Sprintf("%s_USER", r.RegistryName)
	passEnvName := fmt.Sprintf("%s_PASSWORD", r.RegistryName)
	user := os.Getenv(userEnvName)
	pass := os.Getenv(passEnvName)
	if user == "" || pass == "" {
		return "", fmt.Errorf("could not get credentials for %s", r.RegistryName)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", user, pass)))
	return b64, nil
}

func (r RegistryClient) getBearerTokenFromAuthToken() (string, error) {
	authTokenEnvName := fmt.Sprintf("%s_AUTH", r.RegistryName)
	authToken := os.Getenv(authTokenEnvName)
	if authToken == "" {
		t, err := r.getAuthTokenFromCredentials()
		if err != nil {
			return "", err
		}
		if t == "" {
			return "", fmt.Errorf("could not get auth token for %s", r.RegistryName)
		}
		authToken = t
	}
	return r.retrieveBearerToken(authToken)
}

func (r RegistryClient) getBearerToken() (string, error) {
	bearerTokenEnvName := fmt.Sprintf("%s_TOKEN", r.RegistryName)
	bearerToken := os.Getenv(bearerTokenEnvName)
	if bearerToken != "" {
		return bearerToken, nil
	}
	bearerToken, err := r.getBearerTokenFromAuthToken()
	if err != nil {
		// ghcr.io is a special case where we can use GITHUB_TOKEN as the bearer token.
		if r.RegistryName != "GHCR_IO" {
			return "", err
		}
		github_token := os.Getenv("GITHUB_TOKEN")
		if github_token == "" {
			return "", err
		}
		bearerToken = base64.StdEncoding.EncodeToString([]byte(github_token))
	}
	if bearerToken != "" {
		return bearerToken, nil
	}
	return "", fmt.Errorf("could not get a bearer token for %s", r.RegistryName)
}

func (r RegistryClient) IsTagExist(tag string) (bool, error) {
	// First attempt to retrieve tag anonymously, for public images
	if found, err := r.checkManifestForTag("", tag); err == nil {
		return found, nil
	}
	bearerToken, err := r.getBearerToken()
	if err != nil {
		return false, err
	}
	return r.checkManifestForTag(bearerToken, tag)
}