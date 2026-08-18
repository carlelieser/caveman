package anthropic

import (
	"github.com/carlelieser/caveman/internal/adapters"
	"github.com/carlelieser/caveman/internal/ir"
)

const (
	messagesPath = "/v1/messages"
	baseURL      = "https://api.anthropic.com"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Name() string { return "anthropic" }

func (Adapter) Path() string { return messagesPath }

func (Adapter) BaseURL() string { return baseURL }

func (Adapter) ToIR(body adapters.RequestBody) ir.Request { return ToIR(body) }

func (Adapter) FromIR(request ir.Request) adapters.RequestBody { return FromIR(request) }

func (Adapter) ErrorEnvelope(message string) adapters.RequestBody {
	inner := ir.NewObject()
	inner.Set("type", ir.String("invalid_request_error"))
	inner.Set("message", ir.String(message))

	envelope := ir.NewObject()
	envelope.Set("type", ir.String("error"))
	envelope.Set("error", inner)
	return envelope
}

var _ adapters.Provider = Adapter{}
