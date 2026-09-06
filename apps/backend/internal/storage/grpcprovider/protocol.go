package grpcprovider

import "github.com/golang/protobuf/proto"

// These small wire types intentionally mirror proto/storage.proto. They are
// kept private to this transport package so the core never depends on Proton
// or generated provider-specific types.
type accountRequest struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id,json=accountId,proto3" json:"account_id,omitempty"`
}

func (*accountRequest) Reset()           {}
func (m *accountRequest) String() string { return proto.CompactTextString(m) }
func (*accountRequest) ProtoMessage()    {}

type objectRequest struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id,json=accountId,proto3" json:"account_id,omitempty"`
	ObjectID  string `protobuf:"bytes,2,opt,name=object_id,json=objectId,proto3" json:"object_id,omitempty"`
}

func (*objectRequest) Reset()           {}
func (m *objectRequest) String() string { return proto.CompactTextString(m) }
func (*objectRequest) ProtoMessage()    {}

type uploadStart struct {
	AccountID string `protobuf:"bytes,1,opt,name=account_id,json=accountId,proto3" json:"account_id,omitempty"`
	ObjectID  string `protobuf:"bytes,2,opt,name=object_id,json=objectId,proto3" json:"object_id,omitempty"`
	SizeBytes int64  `protobuf:"varint,3,opt,name=size_bytes,json=sizeBytes,proto3" json:"size_bytes,omitempty"`
}

func (*uploadStart) Reset()           {}
func (m *uploadStart) String() string { return proto.CompactTextString(m) }
func (*uploadStart) ProtoMessage()    {}

type uploadRequest struct {
	Start *uploadStart `protobuf:"bytes,1,opt,name=start,proto3,oneof" json:"start,omitempty"`
	Data  []byte       `protobuf:"bytes,2,opt,name=data,proto3,oneof" json:"data,omitempty"`
}

func (*uploadRequest) Reset()           {}
func (m *uploadRequest) String() string { return proto.CompactTextString(m) }
func (*uploadRequest) ProtoMessage()    {}

type uploadResponse struct {
	ObjectID  string `protobuf:"bytes,1,opt,name=object_id,json=objectId,proto3" json:"object_id,omitempty"`
	SizeBytes int64  `protobuf:"varint,2,opt,name=size_bytes,json=sizeBytes,proto3" json:"size_bytes,omitempty"`
}

func (*uploadResponse) Reset()           {}
func (m *uploadResponse) String() string { return proto.CompactTextString(m) }
func (*uploadResponse) ProtoMessage()    {}

type downloadResponse struct {
	Data []byte `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
}

func (*downloadResponse) Reset()           {}
func (m *downloadResponse) String() string { return proto.CompactTextString(m) }
func (*downloadResponse) ProtoMessage()    {}

type objectInfo struct {
	ObjectID   string     `protobuf:"bytes,1,opt,name=object_id,json=objectId,proto3" json:"object_id,omitempty"`
	SizeBytes  int64      `protobuf:"varint,2,opt,name=size_bytes,json=sizeBytes,proto3" json:"size_bytes,omitempty"`
	ModifiedAt *timestamp `protobuf:"bytes,3,opt,name=modified_at,json=modifiedAt,proto3" json:"modified_at,omitempty"`
}

func (*objectInfo) Reset()           {}
func (m *objectInfo) String() string { return proto.CompactTextString(m) }
func (*objectInfo) ProtoMessage()    {}

type timestamp struct {
	Seconds int64 `protobuf:"varint,1,opt,name=seconds,proto3" json:"seconds,omitempty"`
	Nanos   int32 `protobuf:"varint,2,opt,name=nanos,proto3" json:"nanos,omitempty"`
}

func (*timestamp) Reset()           {}
func (m *timestamp) String() string { return proto.CompactTextString(m) }
func (*timestamp) ProtoMessage()    {}

type storageUsage struct {
	TotalBytes int64 `protobuf:"varint,1,opt,name=total_bytes,json=totalBytes,proto3" json:"total_bytes,omitempty"`
	UsedBytes  int64 `protobuf:"varint,2,opt,name=used_bytes,json=usedBytes,proto3" json:"used_bytes,omitempty"`
	FreeBytes  int64 `protobuf:"varint,3,opt,name=free_bytes,json=freeBytes,proto3" json:"free_bytes,omitempty"`
}

func (*storageUsage) Reset()           {}
func (m *storageUsage) String() string { return proto.CompactTextString(m) }
func (*storageUsage) ProtoMessage()    {}

type healthResponse struct {
	Healthy bool `protobuf:"varint,1,opt,name=healthy,proto3" json:"healthy,omitempty"`
}

func (*healthResponse) Reset()           {}
func (m *healthResponse) String() string { return proto.CompactTextString(m) }
func (*healthResponse) ProtoMessage()    {}

type empty struct{}

func (*empty) Reset()           {}
func (m *empty) String() string { return proto.CompactTextString(m) }
func (*empty) ProtoMessage()    {}

type legacyProtoCodec struct{}

func (legacyProtoCodec) Name() string { return "proto" }
func (legacyProtoCodec) Marshal(value any) ([]byte, error) {
	return proto.Marshal(value.(proto.Message))
}
func (legacyProtoCodec) Unmarshal(data []byte, value any) error {
	return proto.Unmarshal(data, value.(proto.Message))
}
