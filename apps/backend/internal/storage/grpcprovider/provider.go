// Package grpcprovider implements the provider-neutral client for a storage
// adapter speaking proto/storage.proto. It contains no Proton SDK knowledge.
package grpcprovider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	maxDataFrameBytes = 4 * 1024 * 1024
	serviceName       = "shardrive.storage.v1.StorageAdapter"
)

type Provider struct {
	conn *grpc.ClientConn
}

func New(address string, opts ...grpc.DialOption) (*Provider, error) {
	if address == "" {
		return nil, fmt.Errorf("create grpc provider: %w: empty address", storage.ErrInvalidRequest)
	}
	if len(opts) == 0 {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial storage adapter: %w", err)
	}
	return &Provider{conn: conn}, nil
}

// NewFromConn is useful for tests and for applications that own connection
// lifecycle. The caller must not close conn while the provider is in use.
func NewFromConn(conn *grpc.ClientConn) (*Provider, error) {
	if conn == nil {
		return nil, fmt.Errorf("create grpc provider: %w: nil connection", storage.ErrInvalidRequest)
	}
	return &Provider{conn: conn}, nil
}

func (p *Provider) Close() error { return p.conn.Close() }

func (p *Provider) Upload(ctx context.Context, storageAccount account.Account, request storage.UploadRequest) (storage.StoredObject, error) {
	if request.Body == nil || request.SizeBytes < 0 || request.ObjectID == "" {
		return storage.StoredObject{}, fmt.Errorf("grpc upload: %w", storage.ErrInvalidRequest)
	}
	if err := validateAccount(storageAccount); err != nil {
		return storage.StoredObject{}, err
	}
	stream, err := p.conn.NewStream(ctx, &grpc.StreamDesc{StreamName: "Upload", ClientStreams: true}, "/"+serviceName+"/Upload", grpc.ForceCodec(legacyProtoCodec{}))
	if err != nil {
		return storage.StoredObject{}, mapError(err)
	}
	if err := stream.SendMsg(&uploadRequest{Start: &uploadStart{AccountID: remoteAccountID(storageAccount), ObjectID: request.ObjectID, SizeBytes: request.SizeBytes}}); err != nil {
		return storage.StoredObject{}, mapError(err)
	}
	buffer := make([]byte, maxDataFrameBytes)
	for {
		read, readErr := request.Body.Read(buffer)
		if read > 0 {
			if err := stream.SendMsg(&uploadRequest{Data: append([]byte(nil), buffer[:read]...)}); err != nil {
				return storage.StoredObject{}, mapError(err)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return storage.StoredObject{}, fmt.Errorf("grpc upload read: %w", readErr)
			}
			break
		}
	}
	if err := stream.CloseSend(); err != nil {
		return storage.StoredObject{}, mapError(err)
	}
	response := new(uploadResponse)
	if err := stream.RecvMsg(response); err != nil {
		return storage.StoredObject{}, mapError(err)
	}
	if response.ObjectID == "" || response.SizeBytes != request.SizeBytes {
		return storage.StoredObject{}, fmt.Errorf("grpc upload: %w: invalid adapter response", storage.ErrInvalidRequest)
	}
	return storage.StoredObject{ObjectID: response.ObjectID, SizeBytes: response.SizeBytes}, nil
}

func (p *Provider) Download(ctx context.Context, storageAccount account.Account, objectID string) (io.ReadCloser, error) {
	if err := validateAccount(storageAccount); err != nil {
		return nil, err
	}
	if objectID == "" {
		return nil, fmt.Errorf("grpc download: %w: empty object id", storage.ErrInvalidRequest)
	}
	stream, err := p.conn.NewStream(ctx, &grpc.StreamDesc{StreamName: "Download", ServerStreams: true}, "/"+serviceName+"/Download", grpc.ForceCodec(legacyProtoCodec{}))
	if err != nil {
		return nil, mapError(err)
	}
	if err := stream.SendMsg(&objectRequest{AccountID: remoteAccountID(storageAccount), ObjectID: objectID}); err != nil {
		return nil, mapError(err)
	}
	if err := stream.CloseSend(); err != nil {
		return nil, mapError(err)
	}
	return &downloadReader{stream: stream}, nil
}

func (p *Provider) Delete(ctx context.Context, storageAccount account.Account, objectID string) error {
	if err := validateAccount(storageAccount); err != nil {
		return err
	}
	if objectID == "" {
		return fmt.Errorf("grpc delete: %w: empty object id", storage.ErrInvalidRequest)
	}
	return p.invoke(ctx, "Delete", &objectRequest{AccountID: remoteAccountID(storageAccount), ObjectID: objectID}, &empty{})
}

func (p *Provider) Stat(ctx context.Context, storageAccount account.Account, objectID string) (storage.ObjectInfo, error) {
	if err := validateAccount(storageAccount); err != nil {
		return storage.ObjectInfo{}, err
	}
	if objectID == "" {
		return storage.ObjectInfo{}, fmt.Errorf("grpc stat: %w: empty object id", storage.ErrInvalidRequest)
	}
	response := new(objectInfo)
	if err := p.invoke(ctx, "Stat", &objectRequest{AccountID: remoteAccountID(storageAccount), ObjectID: objectID}, response); err != nil {
		return storage.ObjectInfo{}, err
	}
	modified := time.Unix(0, 0)
	if response.ModifiedAt != nil {
		modified = time.Unix(response.ModifiedAt.Seconds, int64(response.ModifiedAt.Nanos))
	}
	return storage.ObjectInfo{ObjectID: response.ObjectID, SizeBytes: response.SizeBytes, ModifiedAt: modified}, nil
}

func (p *Provider) Usage(ctx context.Context, storageAccount account.Account) (storage.StorageUsage, error) {
	if err := validateAccount(storageAccount); err != nil {
		return storage.StorageUsage{}, err
	}
	response := new(storageUsage)
	if err := p.invoke(ctx, "Usage", &accountRequest{AccountID: remoteAccountID(storageAccount)}, response); err != nil {
		return storage.StorageUsage{}, err
	}
	return storage.StorageUsage{TotalBytes: response.TotalBytes, UsedBytes: response.UsedBytes, FreeBytes: response.FreeBytes}, nil
}

func (p *Provider) Health(ctx context.Context, storageAccount account.Account) error {
	if err := validateAccount(storageAccount); err != nil {
		return err
	}
	response := new(healthResponse)
	if err := p.invoke(ctx, "Health", &accountRequest{AccountID: remoteAccountID(storageAccount)}, response); err != nil {
		return err
	}
	if !response.Healthy {
		return storage.ErrUnavailable
	}
	return nil
}

func (p *Provider) invoke(ctx context.Context, method string, request, response any) error {
	if err := p.conn.Invoke(ctx, "/"+serviceName+"/"+method, request, response, grpc.ForceCodec(legacyProtoCodec{})); err != nil {
		return mapError(err)
	}
	return nil
}

type downloadReader struct {
	stream  grpc.ClientStream
	pending []byte
}

func (r *downloadReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		response := new(downloadResponse)
		if err := r.stream.RecvMsg(response); err != nil {
			return 0, mapError(err)
		}
		r.pending = response.Data
	}
	read := copy(buffer, r.pending)
	r.pending = r.pending[read:]
	return read, nil
}
func (r *downloadReader) Close() error { return nil }

func validateAccount(storageAccount account.Account) error {
	if storageAccount.ID == "" {
		return fmt.Errorf("grpc provider: %w: empty account id", storage.ErrInvalidRequest)
	}
	return nil
}

func remoteAccountID(storageAccount account.Account) string {
	if storageAccount.CredentialRef != nil && *storageAccount.CredentialRef != "" {
		return *storageAccount.CredentialRef
	}
	return storageAccount.ID
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	code := status.Code(err)
	switch code {
	case codes.InvalidArgument:
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrInvalidRequest, err)
	case codes.NotFound:
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrObjectNotFound, err)
	case codes.Unauthenticated:
		message := strings.ToLower(status.Convert(err).Message())
		if strings.Contains(message, "requires authentication") || strings.Contains(message, "session is required") {
			return fmt.Errorf("grpc provider: %w: %v", storage.ErrAuthenticationRequired, err)
		}
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrAuthentication, err)
	case codes.PermissionDenied:
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrAuthentication, err)
	case codes.ResourceExhausted:
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrQuotaExceeded, err)
	case codes.Unavailable:
		return fmt.Errorf("grpc provider: %w: %v", storage.ErrUnavailable, err)
	default:
		return err
	}
}
