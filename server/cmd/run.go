package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/llm-operator/common/pkg/db"
	v1 "github.com/llm-operator/file-manager/api/v1"
	"github.com/llm-operator/file-manager/server/internal/config"
	"github.com/llm-operator/file-manager/server/internal/s3"
	"github.com/llm-operator/file-manager/server/internal/server"
	"github.com/llm-operator/file-manager/server/internal/store"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const flagConfig = "config"

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := cmd.Flags().GetString(flagConfig)
		if err != nil {
			return err
		}

		c, err := config.Parse(path)
		if err != nil {
			return err
		}

		if err := c.Validate(); err != nil {
			return err
		}

		if err := run(cmd.Context(), &c); err != nil {
			return err
		}
		return nil
	},
}

func run(ctx context.Context, c *config.Config) error {
	var dbInst *gorm.DB
	var err error
	if c.Debug.Standalone {
		dbInst, err = gorm.Open(sqlite.Open(c.Debug.SqlitePath), &gorm.Config{})
	} else {
		dbInst, err = db.OpenDB(c.Database)
	}
	if err != nil {
		return err
	}

	st := store.New(dbInst)
	if err := st.AutoMigrate(); err != nil {
		return err
	}

	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			// Do not use the camel case for JSON fields to follow OpenAI API.
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:     true,
				EmitDefaultValues: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
		runtime.WithIncomingHeaderMatcher(auth.HeaderMatcher),
	)
	addr := fmt.Sprintf("localhost:%d", c.GRPCPort)
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := v1.RegisterFilesServiceHandlerFromEndpoint(ctx, mux, addr, opts); err != nil {
		return err
	}

	var s3Client server.S3Client
	var pathPrefix string
	if c.Debug.Standalone {
		s3Client = &server.NoopS3Client{}
	} else {
		s3conf := c.ObjectStore.S3
		s3Client = s3.NewClient(s3conf)
		pathPrefix = s3conf.PathPrefix
	}
	s := server.New(st, s3Client, pathPrefix)
	createFile := runtime.MustPattern(runtime.NewPattern(
		1,
		[]int{2, 0, 2, 1},
		[]string{"v1", "files"},
		"",
	))
	mux.Handle("POST", createFile, s.CreateFile)
	getFileContent := runtime.MustPattern(runtime.NewPattern(
		1,
		[]int{2, 0, 2, 1, 1, 0, 4, 1, 5, 2, 2, 3},
		[]string{"v1", "files", "id", "content"},
		"",
	))
	mux.Handle("Get", getFileContent, s.GetFileContent)

	errCh := make(chan error)
	go func() {
		log.Printf("Starting HTTP server on port %d", c.HTTPPort)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.HTTPPort), mux)
	}()

	go func() {
		errCh <- s.Run(ctx, c.GRPCPort, c.AuthConfig)
	}()

	go func() {
		s := server.NewWorkerServiceServer(st)
		errCh <- s.Run(c.WorkerServiceGRPCPort)
	}()

	return <-errCh
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
