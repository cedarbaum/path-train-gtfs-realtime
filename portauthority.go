package pathgtfsrt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	portauthority "github.com/jamespfennell/path-train-gtfs-realtime/proto/portauthority"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	portAuthorityBaseUrl           = "https://www.panynj.gov/"
	portAuthorityIncidentsEndpoint = "bin/portauthority/everbridge/incidents?status=All&department=Path"
)

type PortAuthorityClientImpl struct {
	timeoutPeriod time.Duration
}

func NewPortAuthorityClient(timeout time.Duration) *PortAuthorityClientImpl {
	return &PortAuthorityClientImpl{timeoutPeriod: timeout}
}

func (client *PortAuthorityClientImpl) GetIncidents(_ context.Context) ([]Incident, error) {
	incidentsContent, err := client.getContent(portAuthorityIncidentsEndpoint)
	fmt.Println(string(incidentsContent))

	if err != nil {
		return nil, err
	}

	resp := portauthority.GetIncidentsResponse{}
	err = protojson.Unmarshal(incidentsContent, &resp)

	if err != nil {
		return nil, err
	}

	if resp.Status != "Success" {
		return nil, fmt.Errorf("error getting incidents: %s", resp.Status)
	}

	var incidents []Incident
	for _, incident := range resp.Data {
		incidents = append(incidents, incident)
	}

	return incidents, nil
}

// Get the raw bytes from an endpoint in the API.
func (client PortAuthorityClientImpl) getContent(endpoint string) (bytes []byte, err error) {
	httpClient := &http.Client{Timeout: client.timeoutPeriod}
	fmt.Println("Getting content from " + portAuthorityBaseUrl + endpoint)
	resp, err := httpClient.Get(portAuthorityBaseUrl + endpoint)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		closingErr := resp.Body.Close()
		if err == nil {
			err = closingErr
		}
	}()
	return io.ReadAll(resp.Body)
}
