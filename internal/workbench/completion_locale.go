package workbench

import (
	"motty/internal/completion"
	"motty/internal/i18n"
)

var chineseCommandDescriptions = map[string]string{
	"Build an image from a Dockerfile":                          "根据 Dockerfile 构建镜像",
	"Clone a repository into a new directory":                   "将仓库克隆到新目录",
	"Display one or many resources":                             "显示一个或多个资源",
	"Download objects and refs from another repository":         "从其他仓库下载对象和引用",
	"List containers":                                           "列出容器",
	"List images":                                               "列出镜像",
	"OpenSSH remote login client":                               "OpenSSH 远程登录客户端",
	"OpenSSH secure file copy":                                  "OpenSSH 安全文件复制工具",
	"Print the logs for a container in a pod":                   "输出 Pod 中容器的日志",
	"Show changes between commits":                              "显示提交之间的差异",
	"Show commit logs":                                          "显示提交日志",
	"Show details of a specific resource or group of resources": "显示指定资源或一组资源的详细信息",
	"a fast, versatile, remote (and local) file-copying tool":   "快速、通用的远程及本地文件复制工具",
	"a non-interactive network retriever":                       "非交互式网络下载工具",
	"compile packages and dependencies":                         "编译包及其依赖",
	"copy files and directories":                                "复制文件和目录",
	"gofmt (reformat) package sources":                          "用 gofmt 格式化包的源代码",
	"module maintenance":                                        "维护 Go 模块",
	"output the first part of files":                            "输出文件的开头部分",
	"output the last part of files":                             "输出文件的末尾部分",
	"search for files in a directory hierarchy":                 "在目录层级中查找文件",
	"show the working tree status":                              "查看 Git 工作区的当前状态",
	"tar - an archiving utility":                                "tar 归档工具",
	"test packages":                                             "测试 Go 包",
}

func (state *State) localizeCompletionCandidate(candidate completion.Candidate) completion.Candidate {
	if state.language == i18n.Chinese && candidate.FromCatalog && candidate.Kind == completion.KindCommand {
		if description, ok := chineseCommandDescriptions[candidate.Desc]; ok {
			candidate.Desc = description
		}
	}
	return candidate
}
