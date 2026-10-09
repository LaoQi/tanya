package image

const (
	MsgReadImageFmt       = "已读取 %s（%s，%s）"
	MsgReadImageScaledFmt = "已读取 %s（%s，%s；由 %s 缩放，需要细节可用 region 分块读取）"
	MsgReadImageRegionFmt = "已读取 %s 的区域（原图 %s，区域 (%d,%d) %d×%d，输出 %s，%s）"

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

	MsgRegionInvalid     = "region 的 width/height 需为正整数"
	MsgRegionOutside     = "区域与图像（%s）不相交"
	MsgRegionUnsupported = "区域读取仅支持 PNG/JPEG（GIF/WebP 请整图读取）"
	MsgRegionCropFail    = "读取区域失败: %v"
)

const msgDesc = "读取本地图像文件并交给模型查看（仅 JPEG/PNG/GIF/WebP，单次一张）。" +
	"用于查看截图、照片、图表等；文本文件请用 run_shell 读。" +
	"路径支持 ~ 与相对当前工作区。" +
	"超大图整体读取会被压缩到目标长边，需要细节时可用 region 参数分块读取局部原图。"

func toolDesc() string { return msgDesc }

func readImageParams() string {
	return `{"type":"object","properties":{"path":{"type":"string","description":"图像文件路径（~ 或相对当前工作区）"},"detail":{"type":"string","description":"细节档位 low/high/original/auto，缺省用配置"},"region":{"type":"object","description":"只读取的矩形区域，原图像素坐标、左上为原点（与本工具返回的宽高同坐标系），越界部分自动裁剪到图像内；超大图可分块读取保留细节","properties":{"x":{"type":"integer","minimum":0},"y":{"type":"integer","minimum":0},"width":{"type":"integer","minimum":1},"height":{"type":"integer","minimum":1}},"required":["x","y","width","height"],"additionalProperties":false}},"required":["path"]}`
}
