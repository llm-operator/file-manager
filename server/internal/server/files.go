package server

import (
	"context"
	"errors"

	"github.com/google/uuid"
	v1 "github.com/llm-operator/file-manager/api/v1"
	"github.com/llm-operator/file-manager/common/pkg/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const (
	fakeTenantID = "fake-tenant-id"

	purposeFineTune   = "fine-tune"
	purposeAssistants = "assistants"
)

func (s *S) CreateFile(
	ctx context.Context,
	req *v1.CreateFileRequest,
) (*v1.File, error) {
	if len(req.File) == 0 {
		return nil, status.Error(codes.InvalidArgument, "file is required")
	}
	if req.Purpose == "" {
		return nil, status.Error(codes.InvalidArgument, "purpose is required")
	}
	if err := validatePurpose(req.Purpose); err != nil {
		return nil, err
	}

	fileID := newFileID()

	f, err := s.store.CreateFile(store.FileSpec{
		Key: store.FileKey{
			FileID:   fileID,
			TenantID: fakeTenantID,
		},
		Purpose: req.Purpose,
		// TODO(kenji): Fill Filename.
		Bytes: int64(len(req.File)),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create file: %s", err)
	}

	return toFileProto(f), nil
}

// ListFiles lists files.
func (s *S) ListFiles(
	ctx context.Context,
	req *v1.ListFilesRequest,
) (*v1.ListFilesResponse, error) {
	var fs []*store.File
	var err error
	if p := req.Purpose; p != "" {
		if err := validatePurpose(p); err != nil {
			return nil, err
		}
		fs, err = s.store.ListFilesByTenantIDAndPurpose(fakeTenantID, p)
	} else {
		fs, err = s.store.ListFilesByTenantID(fakeTenantID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list files: %s", err)
	}

	var fileProtos []*v1.File
	for _, f := range fs {
		fileProtos = append(fileProtos, toFileProto(f))
	}
	return &v1.ListFilesResponse{
		Object: "list",
		Data:   fileProtos,
	}, nil
}

// GetFile gets a file.
func (s *S) GetFile(
	ctx context.Context,
	req *v1.GetFileRequest,
) (*v1.File, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	m, err := s.store.GetFile(store.FileKey{
		FileID:   req.Id,
		TenantID: fakeTenantID,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(codes.NotFound, "file %q not found", req.Id)
		}
		return nil, status.Errorf(codes.Internal, "get file: %s", err)
	}
	return toFileProto(m), nil
}

// DeleteFile deletes a file.
func (s *S) DeleteFile(
	ctx context.Context,
	req *v1.DeleteFileRequest,
) (*v1.DeleteFileResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	if err := s.store.DeleteFile(store.FileKey{
		FileID:   req.Id,
		TenantID: fakeTenantID,
	}); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(codes.NotFound, "file %q not found", req.Id)
		}
		return nil, status.Errorf(codes.Internal, "delete file: %s", err)
	}
	return &v1.DeleteFileResponse{
		Id:      req.Id,
		Object:  "file",
		Deleted: true,
	}, nil
}

func validatePurpose(p string) error {
	switch p {
	case purposeFineTune, purposeAssistants:
		return nil
	default:
		return status.Errorf(codes.InvalidArgument, "invalid purpose")
	}
}

func toFileProto(f *store.File) *v1.File {
	return &v1.File{
		Id:        f.FileID,
		Bytes:     f.Bytes,
		CreatedAt: f.CreatedAt.UTC().Unix(),
		Filename:  f.Filename,
		Object:    "file",
		Purpose:   f.Purpose,
	}
}

func newFileID() string {
	return uuid.New().String()
}