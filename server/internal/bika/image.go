package bika

import "strings"

// PictureType 决定图片 CDN 域的替换策略。
type PictureType string

const (
	PictureCover    PictureType = "cover"
	PictureCreator  PictureType = "creator"
	PictureFavorite PictureType = "favourite"
	PictureComic    PictureType = "comic"
	PictureElse     PictureType = "else"
)

// ImageURL 复刻插件的 buildBikaImageUrl：按图片类型/线路选择合适的 CDN 域，
// 并规整静态路径前缀。fileServer/path 任一为空时返回空串。
func ImageURL(fileServer, pathValue string, pt PictureType) string {
	u := strings.TrimSpace(fileServer)
	p := strings.TrimSpace(pathValue)
	if u == "" || p == "" {
		return ""
	}
	proxy := 3

	switch u {
	case "https://storage1.picacomic.com":
		switch pt {
		case PictureCover:
			u = "https://img.picacomic.com"
		case PictureCreator, PictureFavorite:
			if proxy == 1 {
				u = "https://storage.diwodiwo.xyz"
			} else {
				u = "https://s3.picacomic.com"
			}
		default:
			if imageQuality != "original" {
				u = "https://img.picacomic.com"
			} else if proxy == 1 {
				u = "https://storage.diwodiwo.xyz"
			} else {
				u = "https://s3.picacomic.com"
			}
		}
	case "https://storage-b.picacomic.com":
		switch {
		case pt == PictureCreator:
			u = "https://storage-b.picacomic.com"
		case pt == PictureCover:
			u = "https://img.picacomic.com"
		case imageQuality == "original":
			u = "https://storage-b.diwodiwo.xyz"
		default:
			u = "https://img.picacomic.com"
		}
	}

	switch {
	case strings.Contains(p, "picacomic-paint.jpg"), strings.Contains(p, "picacomic-gift.jpg"):
		u = "https://s3.picacomic.com/static"
	case strings.Contains(p, "tobeimg/"):
		p = strings.Replace(p, "tobeimg/", "", 1)
	case strings.Contains(p, "tobs/"):
		p = "static/" + strings.Replace(p, "tobs/", "", 1)
	case !strings.Contains(p, "/") && !strings.Contains(u, "static"):
		p = "static/" + p
	}
	return u + "/" + p
}
