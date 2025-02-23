package output

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/bigquery/storage/managedwriter"
	"cloud.google.com/go/bigquery/storage/managedwriter/adapt"
	"github.com/redpanda-data/benthos/v4/public/service"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
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
		return err
	}

	o, err := result.GetResult(ctx)
	if err != nil {
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
