package output

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/googleapis/gax-go/v2/apierror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/bigquery/storage/managedwriter"
	"cloud.google.com/go/bigquery/storage/managedwriter/adapt"
	"github.com/redpanda-data/benthos/v4/public/service"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/cloud/bigquery/storage/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type gcpBigQueryOutputConfig struct {
	ProjectID       string
	DatasetID       string
	TableID         string
	AllowPartial    bool
	DiscardUnknown  bool
	CredentialsJSON string
}

func gcpBigQueryOutputConfigFromParsed(conf *service.ParsedConfig) (gconf gcpBigQueryOutputConfig, err error) {
	if gconf.ProjectID, err = conf.FieldString("project"); err != nil {
		return
	}
	if gconf.ProjectID == "" {
		gconf.ProjectID = bigquery.DetectProjectID
	}
	if gconf.DatasetID, err = conf.FieldString("dataset"); err != nil {
		return
	}
	if gconf.TableID, err = conf.FieldString("table"); err != nil {
		return
	}
	if gconf.AllowPartial, err = conf.FieldBool("allow_partial"); err != nil {
		return
	}
	if gconf.DiscardUnknown, err = conf.FieldBool("discard_unknown"); err != nil {
		return
	}
	if gconf.CredentialsJSON, err = conf.FieldString("credentials_json"); err != nil {
		return
	}
	return
}

type gcpBQClientURL string

func (g gcpBQClientURL) NewClient(ctx context.Context, conf gcpBigQueryOutputConfig) (*bigquery.Client, error) {
	if g == "" {
		var err error
		var opt []option.ClientOption
		opt, err = getClientOptionWithCredential(conf.CredentialsJSON, opt)
		if err != nil {
			return nil, err
		}
		return bigquery.NewClient(ctx, conf.ProjectID)
	}
	return bigquery.NewClient(ctx, conf.ProjectID, option.WithoutAuthentication(), option.WithEndpoint(string(g)))
}

type gcpMWClientURL string

func (g gcpMWClientURL) NewClient(ctx context.Context, conf gcpBigQueryOutputConfig) (*managedwriter.Client, error) {
	if g == "" {
		var err error
		var opt []option.ClientOption
		opt, err = getClientOptionWithCredential(conf.CredentialsJSON, opt)
		if err != nil {
			return nil, err
		}
		return managedwriter.NewClient(ctx, conf.ProjectID, opt...)
	}
	return managedwriter.NewClient(ctx,
		conf.ProjectID,
		option.WithoutAuthentication(),
		option.WithEndpoint(string(g)),
		option.WithGRPCDialOption(grpc.WithInsecure()),
	)
}

func getClientOptionWithCredential(credentialsJSON string, opt []option.ClientOption) ([]option.ClientOption, error) {
	if len(credentialsJSON) > 0 {
		opt = append(opt, option.WithCredentialsJSON([]byte(credentialsJSON)))
	}
	return opt, nil
}

func gcpBigQueryConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Beta().
		Categories("GCP", "Services").
		Version("3.55.0").
		Summary(`Sends messages as new rows to a Google Cloud BigQuery table.`).
		Description(`
== Credentials

By default Redpanda Connect will use a shared credentials file when connecting to GCP services. You can find out more in xref:guides:cloud/gcp.adoc[].

This output currently supports only NEWLINE_DELIMITED_JSON format. First json is converted to proto and then send to bigquery.

Learn more about how to use GCP BigQuery with them here:
https://protobuf.dev/programming-guides/json/
https://cloud.google.com/bigquery/docs/write-api#data_type_conversions

Each message may contain multiple elements separated by newlines. For example a single message containing:

` + "```json" + `
{"key": "1"}
{"key": "2"}
` + "```" + `

Is equivalent to two separate messages:

` + "```json" + `
{"key": "1"}
` + "```" + `

And:

` + "```json" + `
{"key": "2"}
` + "```" + `

` + service.OutputPerformanceDocs(true, true)).
		Field(service.NewStringField("project").Description("The project ID of the dataset to insert data to. If not set, it will be inferred from the credentials or read from the GOOGLE_CLOUD_PROJECT environment variable.").Default("")).
		Field(service.NewStringField("dataset").Description("The BigQuery Dataset ID.")).
		Field(service.NewStringField("table").Description("The table to insert messages to.")).
		Field(service.NewBoolField("allow_partial").
			Description("To allow messages that have missing required fields.").
			Default(true)).
		Field(service.NewBoolField("discard_unknown").
			Description("To ignore unknown fields and enum name values.").
			Default(true)).
		Field(service.NewIntField("max_in_flight").
			Description("The maximum number of message batches to have in flight at a given time. Increase this to improve throughput.").
			Default(64)). // TODO: Tune this default
		Field(service.NewStringField("credentials_json").Description("An optional field to set Google Service Account Credentials json.").Secret().Default("")).
		Field(service.NewBatchPolicyField("batching"))
}

func init() {
	err := service.RegisterBatchOutput(
		"gcp_bigquery_stream", gcpBigQueryConfig(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (output service.BatchOutput, batchPol service.BatchPolicy, maxInFlight int, err error) {
			if batchPol, err = conf.FieldBatchPolicy("batching"); err != nil {
				return
			}
			if maxInFlight, err = conf.FieldInt("max_in_flight"); err != nil {
				return
			}
			var gconf gcpBigQueryOutputConfig
			if gconf, err = gcpBigQueryOutputConfigFromParsed(conf); err != nil {
				return
			}
			output, err = newGCPBigQueryOutput(gconf, mgr.Logger())
			return
		})
	if err != nil {
		panic(err)
	}
}

type gcpBigQueryOutput struct {
	conf        gcpBigQueryOutputConfig
	clientURL   gcpBQClientURL
	mwClientURL gcpMWClientURL

	client   *bigquery.Client
	mwClient *managedwriter.Client
	connMut  sync.RWMutex

	managedStream     *managedwriter.ManagedStream
	messageDescriptor protoreflect.MessageDescriptor
	descriptorProto   *descriptorpb.DescriptorProto

	umo *protojson.UnmarshalOptions

	log *service.Logger
}

func newGCPBigQueryOutput(
	conf gcpBigQueryOutputConfig,
	log *service.Logger,
) (*gcpBigQueryOutput, error) {
	g := &gcpBigQueryOutput{
		conf: conf,
		log:  log,
		umo: &protojson.UnmarshalOptions{
			AllowPartial:   conf.AllowPartial,
			DiscardUnknown: conf.DiscardUnknown,
		},
	}

	return g, nil
}

func (g *gcpBigQueryOutput) Connect(ctx context.Context) (err error) {
	g.connMut.Lock()
	defer g.connMut.Unlock()

	var client *bigquery.Client
	if client, err = g.clientURL.NewClient(ctx, g.conf); err != nil {
		err = fmt.Errorf("error creating big query client: %w", err)
		return
	}
	defer func() {
		if err != nil {
			client.Close()
		}
	}()

	var mwClient *managedwriter.Client
	if mwClient, err = g.mwClientURL.NewClient(ctx, g.conf); err != nil {
		err = fmt.Errorf("error creating BigQuery managed writer client: %w", err)
		return
	}
	defer func() {
		if err != nil {
			mwClient.Close()
		}
	}()

	dataset := client.DatasetInProject(g.conf.ProjectID, g.conf.DatasetID)
	if _, err = dataset.Metadata(ctx); err != nil {
		if hasStatusCode(err, http.StatusNotFound) {
			err = fmt.Errorf("dataset does not exist: %v", g.conf.DatasetID)
		} else {
			err = fmt.Errorf("error checking dataset existence: %w", err)
		}
		return
	}

	table := dataset.Table(g.conf.TableID)
	metadata, err := table.Metadata(ctx)
	if err != nil {
		if hasStatusCode(err, http.StatusNotFound) {
			err = fmt.Errorf("table does not exist: %v", g.conf.TableID)
		} else {
			err = fmt.Errorf("error checking table existence: %w", err)
		}
		return
	}

	md, dp, err := getDescriptor(metadata.Schema)
	if err != nil {
		return err
	}

	ms, err := mwClient.NewManagedStream(ctx,
		managedwriter.WithDestinationTable(managedwriter.TableParentFromParts(
			g.conf.ProjectID, g.conf.DatasetID, g.conf.TableID)),
		managedwriter.WithType(managedwriter.DefaultStream),
		managedwriter.WithSchemaDescriptor(dp),
	)
	if err != nil {
		err = fmt.Errorf("error creating BigQuery managed stream: %w", err)
		return
	}
	defer func() {
		if err != nil {
			ms.Close()
		}
	}()

	g.client = client
	g.mwClient = mwClient
	g.managedStream = ms
	g.messageDescriptor = md
	g.descriptorProto = dp

	g.log.Infof("gcp bigquery managed writer connected - %s.%s.%s\n", client.Project(), g.conf.DatasetID, g.conf.TableID)
	return nil
}

func hasStatusCode(err error, code int) bool {
	if e, ok := err.(*googleapi.Error); ok && e.Code == code {
		return true
	}
	return false
}

// setupDynamicDescriptors aids testing when not using a supplied proto
func getDescriptor(schema bigquery.Schema) (protoreflect.MessageDescriptor, *descriptorpb.DescriptorProto, error) {
	convertedSchema, err := adapt.BQSchemaToStorageTableSchema(schema)
	if err != nil {
		return nil, nil, err
	}

	descriptor, err := adapt.StorageSchemaToProto2Descriptor(convertedSchema, "root")
	if err != nil {
		return nil, nil, err
	}
	md, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, nil, err
	}
	dp, err := adapt.NormalizeDescriptor(md)
	if err != nil {
		return nil, nil, err
	}
	return md, dp, nil
}

func (g *gcpBigQueryOutput) WriteBatch(ctx context.Context, batch service.MessageBatch) error {
	// Try to write the batch, with automatic reconnection on TTL expiration
	return g.writeBatchWithRetry(ctx, batch, 0)
}

func (g *gcpBigQueryOutput) writeBatchWithRetry(ctx context.Context, batch service.MessageBatch, retryCount int) error {
	const maxRetries = 2

	g.connMut.RLock()
	ms := g.managedStream
	g.connMut.RUnlock()
	if ms == nil {
		return service.ErrNotConnected
	}

	var batchErr *service.BatchError
	setErr := func(idx int, err error) {
		if batchErr == nil {
			batchErr = service.NewBatchError(batch, err)
		}
		batchErr = batchErr.Failed(idx, err)
	}

	g.log.Debugf("creating pb messages for batch length %d\n", len(batch))
	var rows [][]byte
	for i, msg := range batch {
		msgBytes, err := msg.AsBytes()
		if err != nil {
			setErr(i, err)
			continue
		}
		message := dynamicpb.NewMessage(g.messageDescriptor)
		if err := g.umo.Unmarshal(msgBytes, message); err != nil {
			setErr(i, err)
			continue
		}
		b, err := proto.Marshal(message)
		if err != nil {
			setErr(i, err)
			continue
		}
		rows = append(rows, b)
	}
	g.log.Debugf("created %d pb messages, errors: %b\n", len(rows), batchErr != nil)

	if len(rows) == 0 {
		if batchErr != nil {
			return batchErr
		}
		return nil
	}

	result, err := ms.AppendRows(ctx, rows)
	if err != nil {
		// Check if this is a connection error that requires reconnection
		if g.isReconnectableError(err) && retryCount < maxRetries {
			g.log.Warnf("bigquery stream connection error, attempting to reconnect (attempt %d/%d):", retryCount+1, maxRetries)
			g.logErrorDetails(err)

			// Attempt to reconnect
			if reconnectErr := g.reconnect(ctx); reconnectErr != nil {
				g.log.Errorf("failed to reconnect BigQuery stream: %v", reconnectErr)
				return fmt.Errorf("connection error reconnect failed: %w", reconnectErr)
			}

			// Retry the operation
			return g.writeBatchWithRetry(ctx, batch, retryCount+1)
		}
		return err
	}

	o, err := result.GetResult(ctx)
	if err != nil {
		// Check if this is a connection error that requires reconnection
		if g.isReconnectableError(err) && retryCount < maxRetries {
			g.log.Warnf("bigquery stream connection error on GetResult, attempting to reconnect (attempt %d/%d):", retryCount+1, maxRetries)
			g.logErrorDetails(err)

			// Attempt to reconnect
			if reconnectErr := g.reconnect(ctx); reconnectErr != nil {
				g.log.Errorf("failed to reconnect BigQuery stream: %v", reconnectErr)
				return fmt.Errorf("connection error reconnect failed: %w", reconnectErr)
			}

			// Retry the operation
			return g.writeBatchWithRetry(ctx, batch, retryCount+1)
		}
		return err
	}
	if o != managedwriter.NoStreamOffset {
		return fmt.Errorf("offset mismatch, got %d want %d", o, managedwriter.NoStreamOffset)
	}

	if batchErr != nil {
		return batchErr
	}

	g.log.Debugf("%d rows written\n", len(rows))
	return nil
}

// isReconnectableError checks if the error is related to connection issues that can be resolved by reconnecting
func (g *gcpBigQueryOutput) isReconnectableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for gRPC status codes first
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.Aborted:
			// TTL expiration typically comes with Aborted code
			return true
		case codes.Unavailable:
			// Service unavailable, connection issues, server shutting down
			return true
		case codes.Internal:
			// Internal errors that might be transient connection issues
			return true
		case codes.DeadlineExceeded:
			// Timeout errors that might benefit from reconnection
			return true
		}
	}

	// Check for structured API errors
	if apiErr, ok := apierror.FromError(err); ok {
		// Check if it's a retriable error
		if apiErr.Unwrap() != nil {
			if s, ok := status.FromError(apiErr.Unwrap()); ok {
				switch s.Code() {
				case codes.Aborted, codes.Unavailable, codes.Internal, codes.DeadlineExceeded:
					return true
				}
			}
		}
	}

	// Fallback to string matching for specific known patterns
	errStr := err.Error()
	if strings.Contains(errStr, "connection TTL") && strings.Contains(errStr, "exceeded") {
		return true
	}
	if strings.Contains(errStr, "server_shutting_down") {
		return true
	}

	return false
}

// logErrorDetails provides detailed logging for structured error information
func (g *gcpBigQueryOutput) logErrorDetails(err error) {
	if err == nil {
		return
	}

	// Log gRPC status information
	if s, ok := status.FromError(err); ok {
		g.log.Errorf("gRPC error - Code: %s, Message: %s", s.Code().String(), s.Message())
	}

	// Log structured API error information
	if apiErr, ok := apierror.FromError(err); ok {
		g.log.Errorf("api error - Reason: %s, Domain: %s", apiErr.Reason(), apiErr.Domain())
		if apiErr.Unwrap() != nil {
			g.log.Errorf("wrapped error: %v", apiErr.Unwrap())
		}

		// Extract BigQuery Storage-specific error details
		storageErr := &storage.StorageError{}
		if e := apiErr.Details().ExtractProtoMessage(storageErr); e == nil {
			g.log.Errorf("storage error - Code: %s, Entity: %s", storageErr.GetCode().String(), storageErr.GetEntity())
			if storageErr.GetErrorMessage() != "" {
				g.log.Errorf("storage error message: %s", storageErr.GetErrorMessage())
			}
		}
	}
}

// reconnect closes the existing stream and creates a new one
func (g *gcpBigQueryOutput) reconnect(ctx context.Context) error {
	g.connMut.Lock()
	defer g.connMut.Unlock()

	// Close existing stream
	if g.managedStream != nil {
		g.managedStream.Close()
		g.managedStream = nil
	}

	// Add a small delay to avoid rapid reconnection attempts
	time.Sleep(time.Second)

	// Create new managed stream
	ms, err := g.mwClient.NewManagedStream(ctx,
		managedwriter.WithDestinationTable(managedwriter.TableParentFromParts(
			g.conf.ProjectID, g.conf.DatasetID, g.conf.TableID)),
		managedwriter.WithType(managedwriter.DefaultStream),
		managedwriter.WithSchemaDescriptor(g.descriptorProto),
	)
	if err != nil {
		return fmt.Errorf("error creating new BigQuery managed stream: %w", err)
	}

	g.managedStream = ms
	g.log.Infof("successfully reconnected BigQuery managed stream - %s.%s.%s", g.client.Project(), g.conf.DatasetID, g.conf.TableID)

	return nil
}

func (g *gcpBigQueryOutput) Close(ctx context.Context) error {
	g.connMut.Lock()
	if g.client != nil {
		g.client.Close()
		g.client = nil
	}
	if g.mwClient != nil {
		g.mwClient.Close()
		g.client = nil
	}
	if g.managedStream != nil {
		g.managedStream.Close()
		g.managedStream = nil
	}
	g.connMut.Unlock()
	return nil
}
