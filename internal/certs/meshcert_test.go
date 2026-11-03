package certs

import (
	"reflect"
	"testing"
	"time"
)

func TestNewMeshCert(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	nb, na := now.Add(-24*time.Hour), now.Add(48*time.Hour)

	// wisera's cert0 on 2026-11-03.
	m := NewMeshCert("wisera", "Mesh Router CA", []string{"*.ip.inojob.com", "wisera.box.inojob.com"}, []string{"80.241.218.30"}, nb, na, "wisera", "inojob.com", "ip.inojob.com", now)
	if !m.HopOK || m.HopName != "wisera.box.inojob.com" || !reflect.DeepEqual(m.IPNames, []string{"*.ip.inojob.com"}) || len(m.Legacy) != 0 || len(m.Other) != 0 || m.Renewal != RenewalOK {
		t.Fatalf("%+v", m)
	}

	// A cert0 from before backend 2.0.40 / 2.0.43.
	m = NewMeshCert("wisera", "Mesh Router CA", []string{"*.inojob.com", "*.nip.io", "*.ip.inojob.com", "wisera.inojob.com"}, nil, nb, na, "wisera", "inojob.com", "ip.inojob.com", now)
	if m.HopOK || !reflect.DeepEqual(m.Legacy, []string{"*.inojob.com", "*.nip.io"}) || !reflect.DeepEqual(m.Other, []string{"wisera.inojob.com"}) || m.IPs == nil {
		t.Fatalf("%+v", m)
	}

	// Identity unknown: no hop name to check against.
	m = NewMeshCert("", "", []string{"wisera.box.inojob.com"}, nil, nb, na, "", "", "", now)
	if m.HopName != "" || m.HopOK {
		t.Fatalf("%+v", m)
	}
}
