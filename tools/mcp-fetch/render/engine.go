package render

import (
	"context"
	"reflect"
	"sync"
	"unsafe"

	"github.com/gost-dom/browser/html"
	"github.com/grafana/sobek"
)

// scriptEngine wraps a gost-dom script engine to make two changes that
// a render needs and that gost-dom v0.12.0 has no option for.
//
// The engine reports the sobek runtime behind every script context to a
// callback, so the caller can interrupt the runtime. gost-dom never
// calls sobek's Interrupt. Without the callback, a script that never
// yields, such as `while (true) {}`, keeps executing after the render
// budget expires and pins a core for the life of the process.
//
// The engine also compiles every script without its source map.
// [disableSourceMaps] and [sourceMapTransport] explain why.
type scriptEngine struct {
	inner    html.ScriptEngine
	register func(vm *sobek.Runtime)
}

func (e *scriptEngine) NewHost(opts html.ScriptEngineOptions) html.ScriptHost {
	// The script host downloads scripts and modules with this client.
	// The browser copies the same client for fetch and XHR after
	// NewHost returns. The transport therefore goes on a copy, and the
	// body of a fetch or XHR response stays as the server sent it.
	if opts.HttpClient != nil {
		client := *opts.HttpClient
		client.Transport = sourceMapTransport{inner: client.Transport}
		opts.HttpClient = &client
	}

	return &scriptHost{inner: e.inner.NewHost(opts), register: e.register}
}

type scriptHost struct {
	inner    html.ScriptHost
	register func(vm *sobek.Runtime)
}

func (h *scriptHost) NewContext(bc html.BrowsingContext) html.ScriptContext {
	sc := h.inner.NewContext(bc)
	if vm := sobekRuntime(sc); vm != nil {
		disableSourceMaps(vm)
		h.register(vm)
	}

	return sc
}

func (h *scriptHost) Close() { h.inner.Close() }

// interruptWatchdog interrupts every registered sobek runtime once ctx
// is done. Interrupt is one of the few sobek calls that is safe from
// another goroutine; the interrupted script aborts with an
// InterruptedError, which the render pipeline already tolerates the
// same way it tolerates any other script error.
type interruptWatchdog struct {
	ctx  context.Context
	stop func() bool
	vms  []*sobek.Runtime
	mu   sync.Mutex
}

func newInterruptWatchdog(ctx context.Context) *interruptWatchdog {
	w := &interruptWatchdog{ctx: ctx}
	w.stop = context.AfterFunc(ctx, w.interruptAll)

	return w
}

func (w *interruptWatchdog) register(vm *sobek.Runtime) {
	w.mu.Lock()
	w.vms = append(w.vms, vm)
	w.mu.Unlock()

	// The context may expire between AfterFunc firing and this
	// registration; interrupting here closes that window. A double
	// Interrupt is harmless.
	if w.ctx.Err() != nil {
		vm.Interrupt(w.ctx.Err())
	}
}

func (w *interruptWatchdog) interruptAll() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, vm := range w.vms {
		vm.Interrupt(w.ctx.Err())
	}
}

// sobekRuntime extracts the unexported vm field from a sobekengine
// script context. Reflection is the only path to the runtime, because
// the engine constructs it privately and no interface exposes it. The
// extraction is defensive. When a gost-dom release changes the layout,
// sobekRuntime returns nil and the render degrades in two ways. It
// abandons the goroutine at budget expiry instead of interrupting it,
// and a source map comment fails an inline script or an eval call.
func sobekRuntime(sc html.ScriptContext) *sobek.Runtime {
	v := reflect.ValueOf(sc)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil
	}

	f := v.Elem().FieldByName("vm")
	if !f.IsValid() || !f.CanAddr() {
		return nil
	}

	//nolint:gosec // G103: unexported-field read; the pointer never outlives the value
	vm, ok := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).
		Elem().Interface().(*sobek.Runtime)
	if !ok {
		return nil
	}

	return vm
}
