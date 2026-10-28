package convert

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Catalog holds every registered converter, caches their availability and
// builds planners.
type Catalog struct {
	all []Converter

	mu       sync.Mutex
	statuses map[string]Status
	priority []string
}

// NewCatalog returns a catalog of converters (in registration order).
func NewCatalog(converters ...Converter) *Catalog { return &Catalog{all: converters} }

// All returns the converters in registration order.
func (c *Catalog) All() []Converter { return c.all }

// Find returns the converter with id, or nil.
func (c *Catalog) Find(id string) Converter {
	for _, x := range c.all {
		if strings.EqualFold(x.ID(), id) {
			return x
		}
	}
	return nil
}

// SetPriority sets the user's engine order (ids, preferred first).
func (c *Catalog) SetPriority(ids []string) {
	c.mu.Lock()
	c.priority = slices.Clone(ids)
	c.mu.Unlock()
}

// Ordered returns the converters in priority order.
func (c *Catalog) Ordered() []Converter {
	out := slices.Clone(c.all)
	slices.SortStableFunc(out, func(a, b Converter) int { return c.priorityIndex(a) - c.priorityIndex(b) })
	return out
}

// Refresh forgets cached statuses (after installing an engine).
func (c *Catalog) Refresh() {
	c.mu.Lock()
	c.statuses = nil
	c.mu.Unlock()
}

// StatusOf returns the cached status of a converter.
func (c *Catalog) StatusOf(x Converter) Status { return c.Statuses()[x.ID()] }

// Statuses checks every converter once and caches the result.
func (c *Catalog) Statuses() map[string]Status {
	c.mu.Lock()
	cached := c.statuses
	c.mu.Unlock()
	if cached != nil {
		return cached
	}
	st := make(map[string]Status, len(c.all))
	var wg sync.WaitGroup
	var smu sync.Mutex
	for _, x := range c.all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := safeStatus(x)
			smu.Lock()
			st[x.ID()] = s
			smu.Unlock()
		}()
	}
	wg.Wait()
	c.mu.Lock()
	c.statuses = st
	c.mu.Unlock()
	return st
}

// Planner plans over the available converters; assumeInstalled ids count as available.
func (c *Catalog) Planner(assumeInstalled ...string) *Planner {
	var use []Converter
	for _, x := range c.all {
		if c.StatusOf(x).Available || slices.Contains(assumeInstalled, x.ID()) {
			use = append(use, x)
		}
	}
	return NewPlanner(use, func(x Converter) int { return min(c.priorityIndex(x), 9) })
}

func (c *Catalog) priorityIndex(x Converter) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, id := range c.priority {
		if strings.EqualFold(id, x.ID()) {
			return i
		}
	}
	return len(c.priority)
}

func safeStatus(x Converter) (s Status) {
	defer func() {
		if r := recover(); r != nil {
			s = Unavailable("engine.reason.error", fmt.Sprint(r))
		}
	}()
	return x.Status()
}
