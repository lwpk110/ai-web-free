package httpclient

import (
	"io"
	"net/http"
)

type HttpClient interface {
	Request(method HttpMethod, url string, headers Headers, cookies []*http.Cookie, body io.Reader) (*http.Response, error)
	SetProxy(url string) error
	SetCookies(rawUrl string, cookies []*http.Cookie)
	GetCookies(rawUrl string) []*http.Cookie
}

type HttpMethod string

const (
	GET     HttpMethod = "GET"
	POST    HttpMethod = "POST"
	PUT     HttpMethod = "PUT"
	HEAD    HttpMethod = "HEAD"
	DELETE  HttpMethod = "DELETE"
	OPTIONS HttpMethod = "OPTIONS"
)

type Headers map[string]string

func (a Headers) Set(key, value string) {
	a[key] = value
}
