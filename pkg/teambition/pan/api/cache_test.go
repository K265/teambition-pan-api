package api

import (
	"testing"
)

func TestCache(t *testing.T) {
	c, _ := NewCache(2)
	c.Put("a", "1")
	if _, ok := c.Get("a"); !ok {
		t.Errorf(`failed to get "%s"`, "a")
	}
	c.Put("b", "1")
	c.Put("c", "1")
	if v, ok := c.Get("a"); ok {
		t.Errorf(`"%s" should be cleaned, but still get "%s"`, "a", v)
	}
}
