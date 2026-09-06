package grpcprovider

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestProviderStreamsAgainstProtoService(t *testing.T) {
	server := grpc.NewServer(grpc.ForceServerCodec(legacyProtoCodec{}))
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: (*storageService)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Delete", Handler: unaryHandler(func(request any) (any, error) { return &empty{}, nil })},
			{MethodName: "Stat", Handler: unaryHandler(func(request any) (any, error) {
				return &objectInfo{ObjectID: request.(*objectRequest).ObjectID, SizeBytes: 3}, nil
			})},
			{MethodName: "Usage", Handler: unaryHandler(func(request any) (any, error) {
				return &storageUsage{TotalBytes: 100, UsedBytes: 3, FreeBytes: 97}, nil
			})},
			{MethodName: "Health", Handler: unaryHandler(func(request any) (any, error) { return &healthResponse{Healthy: true}, nil })},
		},
		Streams: []grpc.StreamDesc{
			{StreamName: "Upload", Handler: uploadHandler, ClientStreams: true},
			{StreamName: "Download", Handler: downloadHandler, ServerStreams: true},
		},
	}, testStorageService{})
	listener := bufconn.Listen(1024 * 1024)
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); listener.Close() })

	conn, err := grpc.NewClient("passthrough:///test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewFromConn(conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	storageAccount := account.Account{ID: "account-1"}

	stored, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{ObjectID: "object-1", SizeBytes: 3, Body: strings.NewReader("abc")})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if stored.ObjectID != "object-1" || stored.SizeBytes != 3 {
		t.Fatalf("stored = %+v", stored)
	}
	reader, err := provider.Download(context.Background(), storageAccount, "object-1")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if string(data) != "abc" {
		t.Fatalf("download = %q", data)
	}
	if usage, err := provider.Usage(context.Background(), storageAccount); err != nil || usage.FreeBytes != 97 {
		t.Fatalf("Usage = %+v, %v", usage, err)
	}
	if err := provider.Health(context.Background(), storageAccount); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestMapErrorDistinguishesRequiredSession(t *testing.T) {
	err := mapError(status.Error(codes.Unauthenticated, "Proton operation requires authentication"))
	if !errors.Is(err, storage.ErrAuthenticationRequired) {
		t.Fatalf("mapped error = %v, want authentication required", err)
	}
	failed := mapError(status.Error(codes.Unauthenticated, "access token rejected"))
	if !errors.Is(failed, storage.ErrAuthentication) {
		t.Fatalf("mapped error = %v, want authentication failed", failed)
	}
}

type storageService interface{}
type testStorageService struct{}

func unaryHandler(handler func(any) (any, error)) grpc.MethodHandler {
	return func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
		request := new(objectRequest)
		if err := dec(request); err != nil {
			return nil, err
		}
		return handler(request)
	}
}

func uploadHandler(_ any, stream grpc.ServerStream) error {
	request := new(uploadRequest)
	var objectID string
	var size int64
	for {
		if err := stream.RecvMsg(request); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if request.Start != nil {
			objectID, size = request.Start.ObjectID, request.Start.SizeBytes
		}
		request = new(uploadRequest)
	}
	return stream.SendMsg(&uploadResponse{ObjectID: objectID, SizeBytes: size})
}

func downloadHandler(_ any, stream grpc.ServerStream) error {
	request := new(objectRequest)
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	return stream.SendMsg(&downloadResponse{Data: []byte("abc")})
}
