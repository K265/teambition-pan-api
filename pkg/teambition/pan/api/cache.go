package api

import (
	"fmt"
	lru "github.com/hashicorp/golang-lru"
)

type Cache interface {
	Get(string) (string, bool)
	Put(string, string)
}

type CacheImpl struct {
	cache *lru.Cache
}

func NewCache(size int) (Cache, error) {
	c, err := lru.New(size)
	if err != nil {
		return nil, err
	}
	return &CacheImpl{cache: c}, nil
}

func (c *CacheImpl) Get(key string) (string, bool) {
	value, ok := c.cache.Get(key)
	return fmt.Sprintf("%s", value), ok
}

func (c *CacheImpl) Put(key string, value string) {
	c.cache.Add(key, value)
}
