// codec.go registers a JSON codec used by the lightweight phase 1 gRPC layer.
package etcdiscv1

import (
	"encoding/json"

	"google.golang.org/grpc/encoding"
)

const jsonCodecName = "json"

// JSONCodecName returns the registered codec name used by the phase 1 gRPC layer.
func JSONCodecName() string {
	return jsonCodecName
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return jsonCodecName
}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}
