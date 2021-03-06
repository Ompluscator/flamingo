package config

import "context"

type (
	// TemplateFunc allows to retrieve config variables
	TemplateFunc struct {
		area *Area
	}
)

// Inject dependencies
func (c *TemplateFunc) Inject(area *Area) {
	c.area = area
}

// Func returns the template function
func (c *TemplateFunc) Func(ctx context.Context) interface{} {
	return func(what string) interface{} {
		val, _ := c.area.Config(what)
		return val
	}
}
