package api

import "fmt"

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
	Size        int    `json:"size,omitempty"`
	Updated     string `json:"updated"`
}

func (n Node) String() string {
	return fmt.Sprintf("Node{Name: %s, NodeId: %s}", n.Name, n.NodeId)
}

type Nodes struct {
	Data []Node `json:"data"`
}

type UploadResult struct {
	NodeId    string   `json:"nodeId"`
	UploadId  string   `json:"uploadId"`
	UploadUrl []string `json:"uploadUrl"`
}
