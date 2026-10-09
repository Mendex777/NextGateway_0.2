package main

import "testing"

func TestNetworkValidationAndDevices(t *testing.T) {
	n := GatewayNetwork{"enp2s0", "10.20.30.10", "10.20.30.0/24", "10.20.30.1", "", ""}
	if e := n.validate(); e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"10.20.30.0", "10.20.30.255", "10.20.30.1", "10.20.30.10", "192.168.1.56", "10.20.31.2"} {
		if validDeviceIP(ip, n) {
			t.Fatalf("invalid device accepted: %s", ip)
		}
	}
	if !validDeviceIP("10.20.30.56", n) {
		t.Fatal("device from new subnet rejected")
	}
	invalid := []GatewayNetwork{
		{"enp2s0\";bad", "10.20.30.10", "10.20.30.0/24", "10.20.30.1", "", ""},
		{"enp2s0", "10.20.30.10", "10.20.30.10/24", "10.20.30.1", "", ""},
		{"enp2s0", "10.20.30.10", "10.20.30.0/24", "10.20.31.1", "", ""},
		{"enp2s0", "10.20.30.10", "10.20.30.0/24", "10.20.30.10", "", ""},
	}
	for _, v := range invalid {
		if v.validate() == nil {
			t.Fatalf("invalid network accepted: %+v", v)
		}
	}
}

func TestSavedNetworkIsIncludedInJob(t *testing.T) {
	configDatabase(t)
	n := GatewayNetwork{"enp2s0", "10.20.30.10", "10.20.30.0/24", "10.20.30.1", "", ""}
	if e := saveNetwork(n); e != nil {
		t.Fatal(e)
	}
	if gatewayNetwork() != n {
		t.Fatal("saved network lost")
	}
}
