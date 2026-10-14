// Copyright (c) 2026 Aristarh Ucolov.
package upnp

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "93.184.216.34": true,
		"192.168.0.1": false, "10.1.2.3": false, "172.20.0.1": false,
		"100.64.0.1": false, "100.127.255.254": false, // CGNAT
		"127.0.0.1": false, "169.254.1.1": false, "": false, "garbage": false,
	} {
		if got := IsPublic(ip); got != want {
			t.Errorf("IsPublic(%q) = %v, want %v", ip, got, want)
		}
	}
}

const sampleDesc = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <device>
  <friendlyName>Home Router</friendlyName>
  <deviceList><device>
   <deviceList><device>
    <serviceList>
     <service><serviceType>urn:schemas-upnp-org:service:WANPPPConnection:1</serviceType><controlURL>/ppp</controlURL></service>
     <service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><controlURL>/ctl/IPConn</controlURL></service>
    </serviceList>
   </device></deviceList>
  </device></deviceList>
 </device>
</root>`

func TestFindWANServicePrefersIP(t *testing.T) {
	var root xmlRoot
	if err := xml.Unmarshal([]byte(sampleDesc), &root); err != nil {
		t.Fatal(err)
	}
	svc, ok := findWANService(root.Device)
	if !ok || svc.ControlURL != "/ctl/IPConn" || !strings.Contains(svc.ServiceType, "WANIPConnection:1") {
		t.Fatalf("got %+v %v", svc, ok)
	}
}

// A fake router: SOAP calls, error replies and the description document.
func TestSOAPCalls(t *testing.T) {
	var lastAction, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAction = r.Header.Get("SOAPAction")
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		switch {
		case strings.Contains(lastAction, "GetExternalIPAddress"):
			_, _ = w.Write([]byte(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"><NewExternalIPAddress>100.70.1.2</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`))
		case strings.Contains(lastAction, "GetSpecificPortMappingEntry"):
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>714</errorCode><errorDescription>NoSuchEntryInArray</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`))
		case strings.Contains(lastAction, "AddPortMapping"):
			if strings.Contains(lastBody, "<NewLeaseDuration>0<") {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>725</errorCode><errorDescription>OnlyPermanentLeasesSupported</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`))
				return
			}
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:AddPortMappingResponse/></s:Body></s:Envelope>`))
		}
	}))
	defer srv.Close()
	gw := &Gateway{ControlURL: srv.URL + "/ctl", ServiceType: "urn:schemas-upnp-org:service:WANIPConnection:1", LocalIP: "192.168.1.10"}
	ctx := context.Background()
	ip, err := gw.ExternalIP(ctx)
	if err != nil || ip != "100.70.1.2" {
		t.Fatalf("ExternalIP: %q %v", ip, err)
	}
	if IsPublic(ip) {
		t.Fatal("CGNAT WAN address reported public")
	}
	if to, err := gw.MappedTo(ctx, "TCP", 8787); err != nil || to != "" {
		t.Fatalf("MappedTo on missing entry: %q %v", to, err)
	}
	if err := gw.AddPortMapping(ctx, "TCP", 8787, "DayZ Server Manager"); err != nil {
		t.Fatalf("AddPortMapping with lease fallback: %v", err)
	}
	if !strings.Contains(lastBody, "<NewLeaseDuration>3600<") || !strings.Contains(lastBody, "<NewInternalClient>192.168.1.10<") {
		t.Fatalf("unexpected AddPortMapping body: %s", lastBody)
	}
	if !strings.Contains(lastAction, "#AddPortMapping") {
		t.Fatalf("SOAPAction header: %s", lastAction)
	}
}
