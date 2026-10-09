package image

const (
	MsgReadImageFmt = "已读取 %s（%s，%s）"

	MsgImageSizeUnknown = "尺寸未知"
)

const (
	MsgImagePathEmpty = "path 不能为空"
	MsgImageNotFound  = "文件不存在: %s"
	MsgImageNotFile   = "不是常规文件: %s"
	MsgImageTooLarge  = "图像 %s 超过大小上限（%d 字节）"
	MsgImageReadFail  = "读取 %s 失败: %v"
	MsgImageBadFormat = "%s 不是受支持的图像（仅 JPEG/PNG/GIF/WebP）"
	MsgBadDetail      = "无效 detail %q（可选: low/high/original/auto）"
)

const msgDesc = "读取本地图像文件并交给模型查看（仅 JPEG/PNG/GIF/WebP，单次一张）。" +
	"用于查看截图、照片、图表等；文本文件请用 run_shell 读。" +
	"路径支持 ~ 与相对当前工作区。"

func toolDesc() string { return msgDesc }

func readImageParams() string {
	return `{"type":"object","properties":{"path":{"type":"string","description":"图像文件路径（~ 或相对当前工作区）"},"detail":{"type":"string","description":"细节档位 low/high/original/auto，缺省用配置"}},"required":["path"]}`
}
