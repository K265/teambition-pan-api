package api

import (
	"fmt"
	"time"
)

type Personal struct {
	Id        string `json:"_id"`
	CreatorId string `json:"_creatorId"`
}

func (p Personal) String() string {
	return fmt.Sprintf("Personal{Id: %s, CreatorId: %s}", p.Id, p.CreatorId)
}

type Space struct {
	RootId string `json:"rootId"`
}

type Drive struct {
	Data struct {
		DriveId string `json:"driveId"`
	} `json:"data"`
}

type Node struct {
	DownloadUrl string `json:"downloadUrl,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	NodeId      string `json:"nodeId"`
	ParentId    string `json:"parentId,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Updated     string `json:"updated"`
}

func (n Node) String() string {
	return fmt.Sprintf("Node{Name: %s, NodeId: %s}", n.Name, n.NodeId)
}

func (n *Node) GetName() string {
	return n.Name
}

func (n *Node) IsDirectory() bool {
	return n.Kind == "folder"
}

func (n *Node) GetTime() (time.Time, error) {
	layout := "2006-01-02T15:04:05.000Z"
	t, err := time.Parse(layout, n.Updated)

	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

type Nodes struct {
	Data []Node `json:"data"`
}

type UploadResult struct {
	NodeId    string   `json:"nodeId"`
	Name      string   `json:"name"`
	UploadId  string   `json:"uploadId"`
	UploadUrl []string `json:"uploadUrl"`
}
