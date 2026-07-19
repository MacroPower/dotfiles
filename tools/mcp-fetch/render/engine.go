package render

import (
	"context"
	"reflect"
	"sync"
	"unsafe"

	"github.com/gost-dom/browser/html"
	"github.com/grafana/sobek"
)

// interruptEngine wraps a gost-dom script engine so the sobek runtime
// behind every script context it creates is reported to a callback.
// gost-dom (v0.12.0) exposes no interrupt hook and never calls sobek's
// Interrupt, so without this wiring a script that never yields (e.g.
// `while (true) {}`) keeps executing after the render budget expires,
// pinning a core for the life of the process.
type interruptEngine struct {
	inner    html.ScriptEngine
	register func(vm *sobek.Runtime)
}

func (e *interruptEngine) NewHost(opts html.ScriptEngineOptions) html.ScriptHost {
	return &interruptHost{inner: e.inner.NewHost(opts), register: e.register}
}

type interruptHost struct {
	inner    html.ScriptHost
	register func(vm *sobek.Runtime)
}

func (h *interruptHost) NewContext(bc html.BrowsingContext) html.ScriptContext {
	sc := h.inner.NewContext(bc)
	if vm := sobekRuntime(sc); vm != nil {
		h.register(vm)
	}

	return sc
}

func (h *interruptHost) Close() { h.inner.Close() }

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
// script context. Reflection is the only path to the runtime: the
// engine constructs it privately and no interface exposes it. The
// extraction is defensive — any layout change in a future gost-dom
// returns nil and the render degrades to abandoning the goroutine at
// budget expiry instead of interrupting it.
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
