package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := "测试/图片"
	i := strings.LastIndex(s, "/")
	fmt.Println(i)
	fmt.Println(s[:i])
	fmt.Println(s[i+1:])
}
