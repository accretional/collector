package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/accretional/collector/gen/collector"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestListServices_100k tests that ListServices works with 100k+ registrations.
// This is a regression test for the 10k hard-coded limit.
func TestListServices_100k(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scalability test in short mode")
	}

	server, _, _ := setupTestServer(t)

	// Register 100k services across multiple namespaces
	const totalServices = 100000
	const servicesPerNamespace = 50000

	t.Logf("Registering %d services...", totalServices)

	// Register 50k in namespace1, 50k in namespace2
	for i := 0; i < servicesPerNamespace; i++ {
		// Namespace 1
		req1 := &collector.RegisterServiceRequest{
			Namespace: "namespace1",
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String(fmt.Sprintf("Service_%d", i)),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("Method1")},
				},
			},
		}
		if _, err := server.RegisterService(context.Background(), req1); err != nil {
			t.Fatalf("RegisterService failed at %d: %v", i, err)
		}

		// Namespace 2
		req2 := &collector.RegisterServiceRequest{
			Namespace: "namespace2",
			ServiceDescriptor: &descriptorpb.ServiceDescriptorProto{
				Name: proto.String(fmt.Sprintf("Service_%d", i)),
				Method: []*descriptorpb.MethodDescriptorProto{
					{Name: proto.String("Method1")},
				},
			},
		}
		if _, err := server.RegisterService(context.Background(), req2); err != nil {
			t.Fatalf("RegisterService failed at %d: %v", i, err)
		}

		if i > 0 && i%10000 == 0 {
			t.Logf("Progress: %d services registered", i*2)
		}
	}

	t.Logf("All %d services registered", totalServices)

	// List all services - should get all 100k
	allResp, err := server.ListServices(context.Background(), &collector.ListServicesRequest{Namespace: ""})
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if allResp.Status.Code != collector.Status_OK {
		t.Fatalf("ListServices failed: %s", allResp.Status.Message)
	}

	allServices := allResp.Services
	if len(allServices) != totalServices {
		t.Errorf("Expected %d services, got %d (BUG: 10k limit)", totalServices, len(allServices))
		t.Logf("This demonstrates the current 10k hard-coded limit")
	}

	// List services in namespace1 - should get 50k
	ns1Resp, err := server.ListServices(context.Background(), &collector.ListServicesRequest{Namespace: "namespace1"})
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if ns1Resp.Status.Code != collector.Status_OK {
		t.Fatalf("ListServices failed: %s", ns1Resp.Status.Message)
	}

	namespace1Services := ns1Resp.Services
	if len(namespace1Services) != servicesPerNamespace {
		t.Errorf("Expected %d services in namespace1, got %d (BUG: 10k limit)", servicesPerNamespace, len(namespace1Services))
	}

	// Verify all namespace1 services actually belong to namespace1
	for _, s := range namespace1Services {
		if s.Namespace != "namespace1" {
			t.Errorf("Expected namespace 'namespace1', got '%s'", s.Namespace)
		}
	}
}

// TestListProtos_100k tests that ListProtos works with 100k+ registrations.
func TestListProtos_100k(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scalability test in short mode")
	}

	server, _, _ := setupTestServer(t)

	const totalProtos = 100000
	const protosPerNamespace = 50000

	t.Logf("Registering %d protos...", totalProtos)

	// Register 50k in namespace1, 50k in namespace2
	for i := 0; i < protosPerNamespace; i++ {
		// Namespace 1
		req1 := &collector.RegisterProtoRequest{
			Namespace: "namespace1",
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String(fmt.Sprintf("file_%d.proto", i)),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("Message1")},
				},
			},
		}
		if _, err := server.RegisterProto(context.Background(), req1); err != nil {
			t.Fatalf("RegisterProto failed at %d: %v", i, err)
		}

		// Namespace 2
		req2 := &collector.RegisterProtoRequest{
			Namespace: "namespace2",
			FileDescriptor: &descriptorpb.FileDescriptorProto{
				Name: proto.String(fmt.Sprintf("file_%d.proto", i)),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: proto.String("Message1")},
				},
			},
		}
		if _, err := server.RegisterProto(context.Background(), req2); err != nil {
			t.Fatalf("RegisterProto failed at %d: %v", i, err)
		}

		if i > 0 && i%10000 == 0 {
			t.Logf("Progress: %d protos registered", i*2)
		}
	}

	t.Logf("All %d protos registered", totalProtos)

	// List all protos - should get all 100k
	allProtos, err := server.ListProtos(context.Background(), "")
	if err != nil {
		t.Fatalf("ListProtos failed: %v", err)
	}

	if len(allProtos) != totalProtos {
		t.Errorf("Expected %d protos, got %d (BUG: 10k limit)", totalProtos, len(allProtos))
		t.Logf("This demonstrates the current 10k hard-coded limit")
	}

	// List protos in namespace1 - should get 50k
	namespace1Protos, err := server.ListProtos(context.Background(), "namespace1")
	if err != nil {
		t.Fatalf("ListProtos failed: %v", err)
	}

	if len(namespace1Protos) != protosPerNamespace {
		t.Errorf("Expected %d protos in namespace1, got %d (BUG: 10k limit)", protosPerNamespace, len(namespace1Protos))
	}

	// Verify all namespace1 protos actually belong to namespace1
	for _, p := range namespace1Protos {
		if p.Namespace != "namespace1" {
			t.Errorf("Expected namespace 'namespace1', got '%s'", p.Namespace)
		}
	}
}
