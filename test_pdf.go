package main

import (
	"fmt"
	"momapeer/internal/rag"
)

func main() {
	path := "D:\\synology\\SynologyDrive\\rog工作\\算网\\制度\\制度汇编下载20260127\\中国移动IT运维专家管理办法\\中国移动IT运维专家管理办法.pdf"
	text, ext, err := rag.ReadDoc(path)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Success! Ext: %s, Text Len: %d\n", ext, len(text))
	}
}
