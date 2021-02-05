package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
)

var BaseUrl = "https://pan.teambition.com"

type Fs interface {
	CreateFolder(ctx context.Context, parent string, name string) error
	List(ctx context.Context, dir string) (names []Node, err error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Remove(ctx context.Context, path string) error
	CreateFile(ctx context.Context, parent string, name string, size int64, in io.Reader) error
	Rename(ctx context.Context, path string, newName string) error
	Move(ctx context.Context, oldPath string, newPath string) error
}

type Config struct {
	TeambitionSessionId    string
	TeambitionSessionIdSig string
}

func (config Config) String() string {
	return fmt.Sprintf("Config{TeambitionSessionId: %s, TeambitionSessionIdSig: %s}", config.TeambitionSessionId, config.TeambitionSessionIdSig)
}

type Teambition struct {
	pathNodeCache Cache
	config        Config
	orgId         string
	memberId      string
	rootId        string
	rootNode      Node
	driveId       string
	ApiBaseUrl    string
	httpClient    *http.Client
}

func (teambition Teambition) String() string {
	return fmt.Sprintf("Teambition{orgId: %s, memberId: %s}", teambition.orgId, teambition.memberId)
}

func (teambition *Teambition) request(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("TEAMBITION_SESSIONID=%s;TEAMBITION_SESSIONID.sig=%s", teambition.config.TeambitionSessionId, teambition.config.TeambitionSessionIdSig))
	res, err2 := teambition.httpClient.Do(req)
	if err2 != nil {
		return nil, err2
	}
	return res, nil
}

func NewFs(ctx context.Context, config *Config) (Fs, error) {
	cache, cerr := NewCache(256)
	if cerr != nil {
		return nil, cerr
	}

	client := &http.Client{}
	teambition := &Teambition{
		config:        *config,
		ApiBaseUrl:    BaseUrl,
		httpClient:    client,
		pathNodeCache: cache,
	}

	// get orgId, memberId
	{
		res, err := teambition.request(ctx, "GET", "https://www.teambition.com/api/organizations/personal", nil)
		if err != nil {
			return nil, err
		}
		var personal Personal
		err = json.NewDecoder(res.Body).Decode(&personal)
		if err != nil {
			return nil, err
		}
		teambition.orgId = personal.Id
		teambition.memberId = personal.CreatorId
		defer res.Body.Close()
	}

	// get root parentId
	{
		res, err := teambition.request(ctx, "GET", fmt.Sprintf("https://pan.teambition.com/pan/api/spaces?orgId=%s&memberId=%s", teambition.orgId, teambition.memberId), nil)
		var spaces []Space
		if err != nil {
			return nil, err
		}
		err = json.NewDecoder(res.Body).Decode(&spaces)
		if err != nil {
			return nil, err
		}
		if len(spaces) < 1 {
			return nil, errors.New("empty spaces")
		}
		teambition.rootId = spaces[0].RootId
		n := &Node{
			NodeId: teambition.rootId,
			Kind:   "folder",
		}
		teambition.rootNode = *n
		defer res.Body.Close()
	}

	// get driveId
	{
		res, err := teambition.request(ctx, "GET", fmt.Sprintf("https://pan.teambition.com/pan/api/orgs/%s?orgId=%s", teambition.orgId, teambition.orgId), nil)
		var drive Drive
		if err != nil {
			return nil, err
		}
		err = json.NewDecoder(res.Body).Decode(&drive)
		if err != nil {
			return nil, err
		}
		teambition.driveId = drive.Data.DriveId
		defer res.Body.Close()
	}

	return teambition, nil
}

// https://pan.teambition.com/pan/api/nodes?orgId=&driveId=&parentId=
func (teambition *Teambition) getNodes(ctx context.Context, node *Node) (*Nodes, error) {
	format := "https://pan.teambition.com/pan/api/nodes?orgId=%s&driveId=%s&parentId=%s&orderDirection=asc"
	res, err := teambition.request(ctx, "GET", fmt.Sprintf(format, teambition.orgId, teambition.driveId, node.NodeId), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var nodes Nodes
	err = json.NewDecoder(res.Body).Decode(&nodes)
	if err != nil {
		return nil, err
	}
	return &nodes, nil
}

func (teambition *Teambition) findNameNode(ctx context.Context, node *Node, name string) (*Node, error) {
	nodes, err := teambition.getNodes(ctx, node)
	if err != nil {
		return nil, err
	}

	for _, d := range nodes.Data {
		if d.Name == name {
			return &d, nil
		}
	}

	return nil, errors.New(fmt.Sprintf(`Can't find "%s" of "%s"`, name, node))
}

func (teambition *Teambition) findNode(ctx context.Context, path string) (*Node, error) {
	if path == "/" || path == "" {
		return &teambition.rootNode, nil
	}

	i := strings.LastIndex(path, "/")
	if i < 0 {
		return nil, errors.New("can't find parent")
	}
	parent := path[:i]
	name := path[i+1:]
	if i == 0 {
		return teambition.findNameNode(ctx, &teambition.rootNode, name)
	}

	nodeId, ok := teambition.pathNodeCache.Get(parent)
	if !ok {
		node, err := teambition.findNode(ctx, parent)
		if err != nil {
			return nil, err
		}
		nodeId = node.NodeId
		teambition.pathNodeCache.Put(parent, nodeId)
	}

	return teambition.findNameNode(ctx, &Node{NodeId: nodeId}, name)
}

func (teambition *Teambition) List(ctx context.Context, folder string) ([]Node, error) {
	path := strings.TrimSuffix(folder, "/")
	node, err := teambition.findNode(ctx, path)
	if err != nil {
		return nil, err
	}

	nodes, err2 := teambition.getNodes(ctx, node)
	if err2 != nil {
		return nil, err2
	}

	return nodes.Data, nil
}

func (teambition *Teambition) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	node, err := teambition.findNode(ctx, path)
	if err != nil {
		return nil, err
	}

	downloadUrl := node.DownloadUrl
	if downloadUrl == "" {
		return nil, errors.New(fmt.Sprintf(`Can't find downloadUrl of %s`, node))
	}

	res, err := teambition.request(ctx, "GET", downloadUrl, nil)
	if err != nil {
		return nil, err
	}

	return res.Body, nil
}

func (teambition *Teambition) CreateFolder(ctx context.Context, parent string, name string) error {
	node, err := teambition.findNode(ctx, parent)
	if err != nil {
		return err
	}
	body := map[string]string{
		"ccpParentId":   node.NodeId,
		"checkNameMode": "refuse",
		"driveId":       teambition.driveId,
		"name":          name,
		"orgId":         teambition.orgId,
		"parentId":      node.NodeId,
		"spaceId":       teambition.rootId,
		"type":          "folder",
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/folder", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (teambition *Teambition) CreateFile(ctx context.Context, parent string, name string, size int64, in io.Reader) error {
	node, err := teambition.findNode(ctx, parent)
	if err != nil {
		return err
	}

	var uploadResults []UploadResult
	{
		body := map[string]interface{}{
			"orgId":         teambition.orgId,
			"spaceId":       teambition.rootId,
			"parentId":      node.NodeId,
			"checkNameMode": "autoRename",
			"infos": []map[string]interface{}{
				{
					"name":        name,
					"ccpParentId": node.NodeId,
					"driveId":     teambition.driveId,
					"size":        size,
					"chunkCount":  1,
					"contentType": "",
					"type":        "file",
				},
			},
		}
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}

		res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/file", bytes.NewBuffer(b))
		if err != nil {
			return err
		}

		err = json.NewDecoder(res.Body).Decode(&uploadResults)
		if err != nil {
			return err
		}

		if len(uploadResults) < 1 || len(uploadResults[0].UploadUrl) < 1 {
			return errors.New(fmt.Sprintf(`Failed to create "%s", can't extact upload url'`, name))
		}
		defer res.Body.Close()
	}

	uploadUrl := uploadResults[0].UploadUrl[0]
	{
		req, err := http.NewRequestWithContext(ctx, "PUT", uploadUrl, in)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		req.Header.Set("Content-Type", "")
		ursp, err := teambition.httpClient.Do(req)
		if err != nil {
			return err
		}
		b, err := ioutil.ReadAll(ursp.Body)
		fmt.Println(string(b))
		defer ursp.Body.Close()
	}

	{
		body := map[string]interface{}{
			"driveId":   teambition.driveId,
			"orgId":     teambition.orgId,
			"nodeId":    uploadResults[0].NodeId,
			"uploadId":  uploadResults[0].UploadId,
			"ccpFileId": uploadResults[0].NodeId,
		}
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		_, err = teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/complete", bytes.NewBuffer(b))
		if err != nil {
			return err
		}
	}
	return nil
}

func (teambition *Teambition) Remove(ctx context.Context, path string) error {
	if path == "" || path == "/" {
		return errors.New("Can't delete root ")
	}

	node, err := teambition.findNode(ctx, path)
	if err != nil {
		return err
	}
	body := map[string]interface{}{
		"nodeIds": []string{node.NodeId},
		"orgId":   teambition.orgId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/archive", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (teambition *Teambition) Rename(ctx context.Context, path string, newName string) error {
	if path == "" || path == "/" {
		return errors.New("Can't rename root ")
	}

	node, err := teambition.findNode(ctx, path)
	if err != nil {
		return err
	}
	body := map[string]interface{}{
		"orgId":     teambition.orgId,
		"driveId":   teambition.driveId,
		"ccpFileId": node.NodeId,
		"name":      newName,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := teambition.request(ctx, "PUT", fmt.Sprintf("https://pan.teambition.com/pan/api/nodes/%s", node.NodeId), bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

func (teambition *Teambition) Move(ctx context.Context, oldPath string, newPath string) error {
	if oldPath == "" || oldPath == "/" {
		return errors.New("Can't move root ")
	}

	oldNode, err := teambition.findNode(ctx, oldPath)
	if err != nil {
		return err
	}
	newNode, err := teambition.findNode(ctx, newPath)
	if err != nil {
		return err
	}
	body := map[string]interface{}{
		"orgId":     teambition.orgId,
		"driveId":   teambition.driveId,
		"sameLevel": false,
		"ids": []map[string]string{
			{
				"id":        oldNode.NodeId,
				"ccpFileId": oldNode.NodeId,
			},
		},
		"parentId": newNode.NodeId,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := teambition.request(ctx, "POST", "https://pan.teambition.com/pan/api/nodes/move", bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}
