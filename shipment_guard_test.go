package main

import "testing"

func TestShipmentDecision(t *testing.T) {
	tests := []struct {
		name  string
		event shipmentEvent
		want  string
		fails bool
	}{
		{"exception calls model", shipmentEvent{ShipmentID: "S-19", Kind: "exception", Detail: "address mismatch"}, "review_exception", false},
		{"proof recorded without model", shipmentEvent{ShipmentID: "S-20", Kind: "delivered", ProofName: "signature.pdf"}, "record_proof", false},
		{"scan recorded without model", shipmentEvent{ShipmentID: "S-21", Kind: "in_transit"}, "record_event", false},
		{"delivery needs proof", shipmentEvent{ShipmentID: "S-22", Kind: "delivered"}, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classify(tc.event)
			if got != tc.want || (err != nil) != tc.fails {
				t.Fatalf("classify() = %q, %v; want %q, fails=%v", got, err, tc.want, tc.fails)
			}
		})
	}
}
