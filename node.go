package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/components/base"
	"github.com/rulego/rulego/utils/maps"
)

const (
	componentType         = "indexedMedia"
	componentVersion      = "0.2.0"
	maxRequestBytes       = 8 << 20
	maxProducedBytes      = int64(1 << 30)
	defaultIndexTimeout   = 30 * time.Second
	defaultDialTimeout    = 5 * time.Second
	defaultProduceTimeout = 300 * time.Second
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type nodeConfiguration struct {
	Root                string `json:"root" label:"Staging root or ref:// node" required:"true" ref:"primary"`
	FFmpegAddress       string `json:"ffmpegAddress" label:"ffmpeg-over-ip address" ref:"shared"`
	FFmpegSecret        string `json:"ffmpegSecret" label:"ffmpeg-over-ip secret" ref:"shared"`
	IndexTimeoutMs      int64  `json:"indexTimeoutMs" label:"Index timeout (ms)" ref:"shared"`
	FFmpegDialTimeoutMs int64  `json:"ffmpegDialTimeoutMs" label:"ffmpeg dial timeout (ms)" ref:"shared"`
	ProduceTimeoutMs    int64  `json:"produceTimeoutMs" label:"Produce timeout (ms)" ref:"shared"`
}

type indexedMediaNode struct {
	base.SharedNode[*sourceManager]
	Config nodeConfiguration
}

type mediaLease struct {
	SourceKey string               `json:"sourceKey"`
	Video     *mediaRepresentation `json:"video,omitempty"`
	Audio     *mediaRepresentation `json:"audio,omitempty"`
}

// Decode each supplied track strictly: omission is the only absent-track form.
func (source *mediaLease) UnmarshalJSON(data []byte) error {
	var wire struct {
		SourceKey string                 `json:"sourceKey"`
		Video     suppliedRepresentation `json:"video"`
		Audio     suppliedRepresentation `json:"audio"`
	}
	if err := decodeStrictJSON(data, &wire); err != nil {
		return err
	}
	*source = mediaLease{SourceKey: wire.SourceKey, Video: wire.Video.value, Audio: wire.Audio.value}
	return nil
}

// Decode supplied values immediately so repeated JSON keys cannot hide a
// malformed companion or an unknown nested field in an earlier value.
type suppliedRepresentation struct{ value *mediaRepresentation }

func (track *suppliedRepresentation) UnmarshalJSON(data []byte) error {
	if track.value != nil {
		return errors.New("duplicate media representation")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("null media representation")
	}
	var representation mediaRepresentation
	if err := decodeStrictJSON(data, &representation); err != nil {
		return err
	}
	track.value = &representation
	return nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

type selectedTrack struct {
	role           string
	representation mediaRepresentation
}

func (source mediaLease) selectedTracks() []selectedTrack {
	tracks := make([]selectedTrack, 0, 2)
	if source.Video != nil {
		tracks = append(tracks, selectedTrack{role: "video", representation: *source.Video})
	}
	if source.Audio != nil {
		tracks = append(tracks, selectedTrack{role: "audio", representation: *source.Audio})
	}
	return tracks
}

type mediaRepresentation struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Container string            `json:"container"`
	Codec     string            `json:"codec"`
}

type inspectRequest struct {
	Operation string     `json:"operation"`
	Source    mediaLease `json:"source"`
}

type produceRequest struct {
	Operation        string     `json:"operation"`
	Source           mediaLease `json:"source"`
	ExpectedRevision string     `json:"expectedRevision"`
	Segment          int        `json:"segment"`
	StagingDir       string     `json:"stagingDir"`
	MaxBytes         int64      `json:"maxBytes"`
	PublishBy        time.Time  `json:"publishBy"`
}

type inspectResult struct {
	Revision string          `json:"revision"`
	Duration float64         `json:"duration"`
	Segments []segmentResult `json:"segments"`
}

type segmentResult struct {
	Duration float64 `json:"duration"`
}

type produceResult struct {
	Member string `json:"member"`
	Bytes  int64  `json:"bytes"`
}

type nodeError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (*indexedMediaNode) Type() string { return componentType }

func (*indexedMediaNode) Def() types.ComponentForm {
	relations := []string{types.Success, types.Failure}
	return types.ComponentForm{
		Type:          componentType,
		Category:      "external",
		Label:         "Indexed Media",
		Desc:          "Inspect and produce bounded indexed-media VOD members",
		Version:       componentVersion,
		ComponentKind: types.ComponentKindNative,
		RelationTypes: &relations,
	}
}

func (*indexedMediaNode) New() types.Node { return &indexedMediaNode{} }

func (n *indexedMediaNode) Init(ruleConfig types.Config, configuration types.Configuration) error {
	if err := maps.Map2Struct(configuration, &n.Config); err != nil {
		return errors.New("indexedMedia: invalid configuration")
	}
	if strings.TrimSpace(n.Config.Root) == "" {
		return errors.New("indexedMedia: root is required")
	}
	if !strings.HasPrefix(n.Config.Root, types.NodeConfigurationPrefixInstanceId) {
		if err := normalizeOwnerConfiguration(&n.Config); err != nil {
			return err
		}
	}
	if err := n.SharedNode.InitWithClose(ruleConfig, n.Type(), n.Config.Root, true,
		func() (*sourceManager, error) { return newSourceManager(n.Config) },
		func(manager *sourceManager) error { manager.Close(); return nil }); err != nil {
		return err
	}
	n.SharedNode.BindChain(configuration)
	return nil
}

func (n *indexedMediaNode) OnMsg(ruleContext types.RuleContext, msg types.RuleMsg) {
	manager, err := n.SharedNode.GetSafely()
	if err != nil {
		tellNodeFailure(ruleContext, msg, problem("configuration", "indexed media owner is unavailable"))
		return
	}
	operation, request, err := decodeRequest(msg.GetData())
	if err != nil {
		tellNodeFailure(ruleContext, msg, problem("invalid_input", "invalid request"))
		return
	}
	ctx := ruleContext.GetContext()
	if ctx == nil {
		ctx = context.Background()
	}
	switch operation {
	case "inspect":
		result, inspectErr := manager.Inspect(ctx, request.(inspectRequest))
		if inspectErr != nil {
			tellNodeFailure(ruleContext, msg, inspectErr)
			return
		}
		tellNodeSuccess(ruleContext, msg, result)
	case "produce":
		result, produceErr := manager.Produce(ctx, request.(produceRequest))
		if produceErr != nil {
			tellNodeFailure(ruleContext, msg, produceErr)
			return
		}
		tellNodeSuccess(ruleContext, msg, result)
	}
}

func (n *indexedMediaNode) Destroy() { _ = n.SharedNode.Close() }

func normalizeOwnerConfiguration(config *nodeConfiguration) error {
	if strings.TrimSpace(config.FFmpegAddress) == "" || config.FFmpegSecret == "" {
		return errors.New("indexedMedia: ffmpeg connection is required")
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return errors.New("indexedMedia: invalid staging root")
	}
	config.Root = filepath.Clean(root)
	const maxDurationMillis = int64(math.MaxInt64) / int64(time.Millisecond)
	for _, value := range []*int64{&config.IndexTimeoutMs, &config.FFmpegDialTimeoutMs, &config.ProduceTimeoutMs} {
		if *value < 0 || *value > maxDurationMillis {
			return errors.New("indexedMedia: timeout is out of range")
		}
	}
	if config.IndexTimeoutMs == 0 {
		config.IndexTimeoutMs = defaultIndexTimeout.Milliseconds()
	}
	if config.FFmpegDialTimeoutMs == 0 {
		config.FFmpegDialTimeoutMs = defaultDialTimeout.Milliseconds()
	}
	if config.ProduceTimeoutMs == 0 {
		config.ProduceTimeoutMs = defaultProduceTimeout.Milliseconds()
	}
	return nil
}

func decodeRequest(data string) (string, any, error) {
	if len(data) > maxRequestBytes {
		return "", nil, errors.New("request is too large")
	}
	var head struct {
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal([]byte(data), &head); err != nil {
		return "", nil, err
	}
	var target any
	switch head.Operation {
	case "inspect":
		target = &inspectRequest{}
	case "produce":
		target = &produceRequest{}
	default:
		return "", nil, errors.New("unsupported operation")
	}
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return "", nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", nil, err
	}
	switch value := target.(type) {
	case *inspectRequest:
		if err := value.Source.validate(); err != nil {
			return "", nil, err
		}
		return head.Operation, *value, nil
	case *produceRequest:
		if err := value.Source.validate(); err != nil {
			return "", nil, err
		}
		if !revisionPattern.MatchString(value.ExpectedRevision) || value.Segment < 0 || strings.TrimSpace(value.StagingDir) == "" || value.MaxBytes <= 0 || value.MaxBytes > maxProducedBytes || value.PublishBy.IsZero() {
			return "", nil, errors.New("invalid production request")
		}
		return head.Operation, *value, nil
	default:
		panic("unreachable request")
	}
}

func (source mediaLease) validate() error {
	if source.SourceKey == "" || source.SourceKey != strings.TrimSpace(source.SourceKey) || len(source.SourceKey) > 4096 || strings.IndexByte(source.SourceKey, 0) >= 0 {
		return errors.New("invalid source key")
	}
	tracks := source.selectedTracks()
	if len(tracks) == 0 {
		return errors.New("invalid indexed media lease")
	}
	for _, track := range tracks {
		if !track.representation.valid(track.role == "video") {
			return errors.New("invalid indexed media lease")
		}
	}
	if source.Video != nil && source.Audio != nil && source.Video.URL == source.Audio.URL {
		return errors.New("invalid indexed media lease")
	}
	return nil
}

func (representation mediaRepresentation) valid(video bool) bool {
	if representation.URL == "" || len(representation.URL) > 64<<10 || strings.Contains(representation.URL, "|") {
		return false
	}
	parsed, err := url.Parse(representation.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	container, codec := strings.ToLower(representation.Container), strings.ToLower(representation.Codec)
	if video {
		if container != "mp4" || !(strings.HasPrefix(codec, "avc1") || strings.HasPrefix(codec, "h264")) {
			return false
		}
	} else if (container != "m4a" && container != "mp4") || !(strings.HasPrefix(codec, "mp4a.40.") || codec == "aac") {
		return false
	}
	if len(representation.Headers) > 128 {
		return false
	}
	for name, value := range representation.Headers {
		if len(value) > 16<<10 || !validHeader(name, value) {
			return false
		}
	}
	return true
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func tellNodeSuccess(ctx types.RuleContext, input types.RuleMsg, value any) {
	payload, _ := json.Marshal(value)
	output := input.Copy()
	output.DataType = types.JSON
	output.SetBytes(payload)
	ctx.TellSuccess(output)
}

func tellNodeFailure(ctx types.RuleContext, input types.RuleMsg, err error) {
	p := asProblem(err)
	payload, _ := json.Marshal(nodeError{Kind: p.kind, Message: p.message})
	output := input.Copy()
	output.DataType = types.JSON
	output.SetBytes(payload)
	ctx.TellFailure(output, errors.New(p.message))
}

type sourceProblem struct {
	kind    string
	message string
}

func (e *sourceProblem) Error() string { return e.message }

func problem(kind, message string) *sourceProblem {
	return &sourceProblem{kind: kind, message: message}
}

func asProblem(err error) *sourceProblem {
	var p *sourceProblem
	if errors.As(err, &p) {
		return p
	}
	return problem("internal", "indexed media operation failed")
}

func formatSeconds(value float64) string {
	return fmt.Sprintf("%.9f", value)
}
