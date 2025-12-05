package client

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type authTransport struct {
	Host      string
	ApiKey    string
	Transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	req.Header.Add("x-api-key", t.ApiKey)

	// Gather base URL from Host
	baseUrl, err := url.Parse(t.Host)
	if err != nil {
		return nil, errors.New("Unable to parse base URL: " + t.Host)
	}
	// Gather path URL from request
	pathUrl := req.URL.String()
	// Combine
	finalUrl := baseUrl.String() + pathUrl
	req.URL, err = url.Parse(finalUrl)
	if err != nil {
		return nil, errors.New("Unable to combine base URL: " + t.Host + " with path URL: " + pathUrl)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode > 400 {
		return nil, errors.New("Error processing data. Status code: " + strconv.Itoa(resp.StatusCode))
	}

	return resp, nil
}

func NewHttpClient(host, apiKey string) (*http.Client, error) {
	// Use an extremely long timeout for synchronous calls like attaching/detaching networks
	return &http.Client{Timeout: time.Duration(60) * time.Second, Transport: &authTransport{
		Host:      host,
		ApiKey:    apiKey,
		Transport: http.DefaultTransport,
	}}, nil
}
