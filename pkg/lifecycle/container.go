package lifecycle

import (
	"fmt"
	"reflect"
)

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// container manages constructor-based dependency injection. It tracks
// constructor functions, resolves their dependencies via topological sort,
// and caches results as singletons.
type container struct {
	providers []*provider
	values    map[reflect.Type]reflect.Value
	typeToIdx map[reflect.Type]int
}

type provider struct {
	fn      reflect.Value
	params  []reflect.Type
	results []reflect.Type
	hasErr  bool
}

func newContainer() *container {
	return &container{
		values:    make(map[reflect.Type]reflect.Value),
		typeToIdx: make(map[reflect.Type]int),
	}
}

// supply registers a pre-built value directly into the container.
func (c *container) supply(val any) {
	rv := reflect.ValueOf(val)
	c.values[rv.Type()] = rv
}

// provide registers a constructor function. The function's parameter types
// are its dependencies and its return types are what it provides. The last
// return value may be an error.
func (c *container) provide(fn any) error {
	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		return fmt.Errorf("lifecycle: provide: expected function, got %T", fn)
	}

	ft := rv.Type()
	p := &provider{fn: rv}

	for i := 0; i < ft.NumIn(); i++ {
		p.params = append(p.params, ft.In(i))
	}

	numOut := ft.NumOut()
	if numOut == 0 {
		return fmt.Errorf("lifecycle: provide: constructor must return at least one value")
	}

	if ft.Out(numOut - 1).Implements(errorType) {
		p.hasErr = true
		numOut--
	}

	if numOut == 0 {
		return fmt.Errorf("lifecycle: provide: constructor must return at least one non-error value")
	}

	idx := len(c.providers)
	for i := 0; i < numOut; i++ {
		rt := ft.Out(i)
		if _, ok := c.values[rt]; ok {
			return fmt.Errorf("lifecycle: provide: type %v already supplied", rt)
		}
		if _, ok := c.typeToIdx[rt]; ok {
			return fmt.Errorf("lifecycle: provide: type %v already provided", rt)
		}
		p.results = append(p.results, rt)
		c.typeToIdx[rt] = idx
	}

	c.providers = append(c.providers, p)
	return nil
}

// resolve calls all constructors in dependency order using Kahn's algorithm
// for topological sort. It detects missing dependencies and cycles.
func (c *container) resolve() error {
	n := len(c.providers)
	if n == 0 {
		return nil
	}

	// Build dependency edges. deps[i] lists provider indices that i depends on.
	deps := make([][]int, n)
	for i, p := range c.providers {
		seen := make(map[int]bool)
		for _, param := range p.params {
			if _, ok := c.values[param]; ok {
				continue
			}
			j, ok := c.typeToIdx[param]
			if !ok {
				return fmt.Errorf("lifecycle: resolve: missing dependency %v", param)
			}
			if !seen[j] {
				seen[j] = true
				deps[i] = append(deps[i], j)
			}
		}
	}

	// Topological sort (Kahn's algorithm).
	// adj[j] lists providers that depend on j (j must come before them).
	adj := make([][]int, n)
	inDegree := make([]int, n)
	for i, d := range deps {
		inDegree[i] = len(d)
		for _, j := range d {
			adj[j] = append(adj[j], i)
		}
	}

	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	order := make([]int, 0, n)
	for len(queue) > 0 {
		j := queue[0]
		queue = queue[1:]
		order = append(order, j)
		for _, i := range adj[j] {
			inDegree[i]--
			if inDegree[i] == 0 {
				queue = append(queue, i)
			}
		}
	}

	if len(order) != n {
		return fmt.Errorf("lifecycle: resolve: dependency cycle detected")
	}

	// Call constructors in dependency order.
	for _, idx := range order {
		p := c.providers[idx]
		args := make([]reflect.Value, len(p.params))
		for i, param := range p.params {
			args[i] = c.values[param]
		}

		results := p.fn.Call(args)

		if p.hasErr {
			errVal := results[len(results)-1]
			if !errVal.IsNil() {
				return fmt.Errorf("lifecycle: resolve: %w", errVal.Interface().(error))
			}
			results = results[:len(results)-1]
		}

		for i, rt := range p.results {
			c.values[rt] = results[i]
		}
	}

	return nil
}

// call resolves dependencies for fn and calls it. Used for invoke functions.
func (c *container) call(fn any) error {
	rv := reflect.ValueOf(fn)
	if rv.Kind() != reflect.Func {
		return fmt.Errorf("lifecycle: invoke: expected function, got %T", fn)
	}

	ft := rv.Type()
	args := make([]reflect.Value, ft.NumIn())
	for i := 0; i < ft.NumIn(); i++ {
		v, ok := c.values[ft.In(i)]
		if !ok {
			return fmt.Errorf("lifecycle: invoke: missing dependency %v", ft.In(i))
		}
		args[i] = v
	}

	results := rv.Call(args)

	if len(results) > 0 {
		last := results[len(results)-1]
		if last.Type().Implements(errorType) && !last.IsNil() {
			return fmt.Errorf("lifecycle: invoke: %w", last.Interface().(error))
		}
	}

	return nil
}
