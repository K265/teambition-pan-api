package api

import (
	"testing"
)

func TestCache(t *testing.T) {
	c, _ := NewCache(2)
	c.Put("a", &Node{Name: "a"})
	if _, ok := c.Get("a"); !ok {
		t.Errorf(`failed to get "%s"`, "a")
	}
	c.Put("b", &Node{Name: "b"})
	c.Put("c", &Node{Name: "c"})
	if v, ok := c.Get("a"); ok {
		t.Errorf(`"%s" should be cleaned, but still get "%s"`, "a", v)
	}
}
