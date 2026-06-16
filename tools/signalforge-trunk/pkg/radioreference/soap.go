package radioreference

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultSOAPEndpoint = "https://api.radioreference.com/soap2/?wsdl"

// SOAPClient fetches trunk system data from RadioReference Database Web Service.
// Requires developer API key and user premium credentials.
type SOAPClient struct {
	endpoint string
	apiKey   string
	username string
	password string
	http     *http.Client
}

func NewSOAPClient(apiKey, username, password string) *SOAPClient {
	return &SOAPClient{
		endpoint: defaultSOAPEndpoint,
		apiKey:   apiKey,
		username: username,
		password: password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// GetTrunkedSystem fetches system metadata by RadioReference sid.
func (c *SOAPClient) GetTrunkedSystem(sid int) (TrunkSystemResponse, error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <getTrunkedSystem xmlns="http://api.radioreference.com/soap2">
      <authInfo>
        <appKey>%s</appKey>
        <username>%s</username>
        <password>%s</password>
      </authInfo>
      <sid>%d</sid>
    </getTrunkedSystem>
  </soap:Body>
</soap:Envelope>`, xmlEscape(c.apiKey), xmlEscape(c.username), xmlEscape(c.password), sid)

	req, err := http.NewRequest(http.MethodPost, c.soapEndpoint(), bytes.NewBufferString(body))
	if err != nil {
		return TrunkSystemResponse{}, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "getTrunkedSystem")

	resp, err := c.http.Do(req)
	if err != nil {
		return TrunkSystemResponse{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TrunkSystemResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return TrunkSystemResponse{}, fmt.Errorf("soap returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return parseTrunkSystemResponse(data)
}

type TrunkSystemResponse struct {
	SID      int    `xml:"sid"`
	Name     string `xml:"name"`
	SysID    string `xml:"sysid"`
	WACN     string `xml:"wacn"`
	Protocol string `xml:"protocol"`
}

func (c *SOAPClient) soapEndpoint() string {
	ep := c.endpoint
	if strings.HasSuffix(ep, "?wsdl") {
		ep = strings.TrimSuffix(ep, "?wsdl")
	}
	return ep
}

func parseTrunkSystemResponse(data []byte) (TrunkSystemResponse, error) {
	var envelope struct {
		Body struct {
			Response TrunkSystemResponse `xml:"getTrunkedSystemResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return TrunkSystemResponse{}, fmt.Errorf("parse soap response: %w", err)
	}
	if envelope.Body.Response.Name == "" && envelope.Body.Response.SID == 0 {
		return TrunkSystemResponse{}, fmt.Errorf("empty trunk system response; verify API key and premium credentials")
	}
	return envelope.Body.Response, nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// SyncOKWIN updates config system metadata from RR SOAP for sid 6949.
func SyncOKWIN(client *SOAPClient, sid int) (TrunkSystemResponse, error) {
	if sid == 0 {
		sid = 6949
	}
	return client.GetTrunkedSystem(sid)
}
