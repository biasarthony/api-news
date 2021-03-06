package repositories

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/jacky-htg/api-news/config"
	"github.com/jacky-htg/api-news/libraries"
	"github.com/jacky-htg/api-news/models"
	"github.com/nfnt/resize"
)

func NewsList(param map[string]string) ([]models.News, error) {
	var news []models.News
	var query string
	var paramStr = ""

	query = "SELECT " +
		"`news`.`id`, `news`.`title`, `slug`, `content`, `image`, `image_caption`, `status`, `publish_date`, `writer`, `editor`, `news`.`created_at`, `news`.`updated_at`" +
		" FROM `news` JOIN `news_topics` ON (`news`.`id`=`news_topics`.`news_id`) JOIN `topics` ON (`news_topics`.`topic_id`=`topics`.`id`)"

	query += " WHERE "

	if len(param["status"]) > 0 && param["status"] != "" {
		query += " `status`='" + param["status"] + "'"
		paramStr += "status=" + param["status"]
	} else {
		query += " `status`!='X'"
		paramStr += "status!=X"
	}

	if len(param["search"]) > 0 && param["search"] != "" {
		query += " AND (`news`.`title` LIKE '%" + param["search"] + "%' OR `content` LIKE '%" + param["search"] + "%' OR `topics`.`title` LIKE '%" + param["search"] + "%')"
		paramStr += "search=" + param["search"]
	}

	if len(param["topic"]) > 0 && param["topic"] != "" {
		query += " AND `news_topics`.`topic_id`='" + param["topic"] + "'"
		paramStr += "topic=" + param["topic"]
	}

	query += " GROUP BY `news`.`id` "

	if len(param["sortby"]) > 0 && param["sortby"] != "" {
		query += " ORDER BY " + param["sortby"]
		paramStr += "sortBy=" + param["sortBy"]
		if len(param["order"]) > 0 && param["order"] != "" {
			query += " " + param["order"]
			paramStr += "order=" + param["order"]
		} else {
			query += " DESC"
		}
	} else {
		if len(param["order"]) > 0 && param["order"] != "" {
			query += " ORDER BY `news`.`id` " + param["order"]
			paramStr += "order=" + param["order"]
		} else {
			query += " ORDER BY `news`.`id` DESC"
		}
	}

	var offset int
	limit, _ := strconv.Atoi(param["limit"])
	page, _ := strconv.Atoi(param["page"])
	offset = page*limit - limit

	query += " LIMIT " + param["limit"] + " OFFSET " + strconv.Itoa(offset)

	if exists := libraries.RedisExists("newslist:" + paramStr); exists {
		data, err := libraries.RedisGet("newslist:" + paramStr)
		libraries.CheckError(err)
		if err != nil {
			return []models.News{}, err
		}

		if err := json.Unmarshal(data, &news); err != nil {
			libraries.CheckError(err)
			return []models.News{}, err
		}

		return news, nil
	}

	rows, err := db.Query(query)
	return getNewsRow(rows, err, paramStr)
}

func NewsGet(paramID uint) (models.News, error) {
	rows, err := db.Query(
		"SELECT `id`, `title`, `slug`, `content`, `image`, `image_caption`, `status`, `publish_date`, `writer`, `editor`, `created_at`, `updated_at` FROM `news` WHERE `id`=?",
		paramID,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer rows.Close()

	return getNewsByRow(rows)
}

func NewsFindFirst() (models.News, error) {
	rows, err := db.Query("SELECT `id`, `title`, `slug`, `content`, `image`, `image_caption`, `status`, `publish_date`, `writer`, `editor`, `created_at`, `updated_at` FROM `news` WHERE `status` != 'X' ORDER BY id ASC LIMIT 1")
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer rows.Close()

	return getNewsByRow(rows)
}

func NewsFindLast() (models.News, error) {
	rows, err := db.Query("SELECT `id`, `title`, `slug`, `content`, `image`, `image_caption`, `status`, `publish_date`, `writer`, `editor`, `created_at`, `updated_at` FROM `news` WHERE `status` != 'X' ORDER BY id DESC LIMIT 1")
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer rows.Close()

	return getNewsByRow(rows)
}

func getNewsRow(rows *sql.Rows, err error, paramStr string) ([]models.News, error) {
	var news []models.News
	libraries.CheckError(err)

	if err != nil {
		return []models.News{}, err
	}

	defer rows.Close()

	for rows.Next() {
		var o models.News
		var oNull models.NewsNull

		err := rows.Scan(
			&o.ID,
			&o.Title,
			&o.Slug,
			&o.Content,
			&oNull.Image,
			&oNull.ImageCaption,
			&o.Status,
			&oNull.PublishDate,
			&o.Writer.ID,
			&oNull.Editor,
			&o.CreatedAt,
			&o.UpdatedAt,
		)
		libraries.CheckError(err)
		if err != nil {
			return []models.News{}, err
		}

		o.Image = oNull.Image.String
		o.ImageCaption = oNull.ImageCaption.String
		o.PublishDate = oNull.PublishDate.Time
		o.Editor.ID = uint(oNull.Editor.Int64)

		if o.Writer.ID > 0 {
			o.Writer, err = UserGet(o.Writer.ID)
			libraries.CheckError(err)
			if err != nil {
				return []models.News{}, err
			}

			o.Writer.Password = nil
		} else {
			o.Writer = models.User{}
		}

		if o.Editor.ID > 0 {
			o.Editor, err = UserGet(o.Editor.ID)
			libraries.CheckError(err)
			if err != nil {
				return []models.News{}, err
			}

			o.Editor.Password = nil
		} else {
			o.Editor = models.User{}
		}

		o.Topic, err = TopicGetByNewsId(o.ID)
		if err != nil {
			return []models.News{}, err
		}

		news = append(news, o)
	}

	err = rows.Err()
	if err != nil {
		return []models.News{}, err
	}

	jsonData, err := json.Marshal(&news)
	libraries.CheckError(err)
	if err != nil {
		return []models.News{}, err
	}

	if err = libraries.RedisSet("newslist:"+paramStr, string(jsonData), 0); err != nil {
		libraries.CheckError(err)
		return []models.News{}, err
	}

	return news, nil
}

func getNewsByRow(rows *sql.Rows) (models.News, error) {
	var o models.News
	var oNull models.NewsNull

	for rows.Next() {
		err := rows.Scan(
			&o.ID,
			&o.Title,
			&o.Slug,
			&o.Content,
			&o.Image,
			&o.ImageCaption,
			&o.Status,
			&oNull.PublishDate,
			&o.Writer.ID,
			&oNull.Editor,
			&o.CreatedAt,
			&o.UpdatedAt,
		)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}

		o.PublishDate = oNull.PublishDate.Time
		o.Editor.ID = uint(oNull.Editor.Int64)
	}

	err = rows.Err()
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	if o.Writer.ID > 0 {
		o.Writer, err = UserGet(o.Writer.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}

		o.Writer.Password = nil
	} else {
		o.Writer = models.User{}
	}

	if o.Editor.ID > 0 {
		o.Editor, err = UserGet(o.Editor.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}

		o.Editor.Password = nil
	} else {
		o.Editor = models.User{}
	}

	o.Topic, err = TopicGetByNewsId(o.ID)
	if err != nil {
		return models.News{}, err
	}

	return o, nil
}

func NewsIsExists(title string, id uint) (bool, error) {
	var o models.News
	var err error
	var isExist bool = false

	if id > 0 {
		o, err = newsIsExistById(title, id)
	} else {
		o, err = newsIsExist(title)
	}

	if err != nil {
		return isExist, err
	}

	if o.ID > 0 {
		isExist = true
	}

	return isExist, nil
}

func NewsStore(o models.News) (models.News, error) {

	if len(o.Image) > 0 && o.Image[0:10] == "data:image" {
		imagePath, err := imageAndUpload(o.Image, o.ID, o.Writer.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}

		o.Image = imagePath["shared"]

		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}
	}

	stmt, err := db.Prepare(
		"INSERT INTO news (`title`, `slug`, `content`, `image`, `image_caption`, `writer`)" +
			" VALUES (?, ?, ?, ?, ?, ?)",
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer stmt.Close()

	res, err := stmt.Exec(
		o.Title,
		o.Slug,
		o.Content,
		o.Image,
		o.ImageCaption,
		o.Writer.ID,
	)

	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	id, err := res.LastInsertId()
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	o.ID = uint(id)

	err = storeNewsTopic(o)
	if err != nil {
		return o, err
	}

	o, err = NewsGet(o.ID)
	if err != nil {
		return o, err
	}

	err = libraries.RedisDeletePrefix("newslist:")
	libraries.CheckError(err)

	return o, nil
}

func storeNewsTopic(o models.News) error {
	for _, t := range o.Topic {
		topic, err := TopicGet(t.ID)
		if err != nil {
			return err
		}

		stmt, _ := db.Prepare("INSERT INTO news_topics (`news_id`, `topic_id`) VALUES (?, ?)")
		defer stmt.Close()
		_, err = stmt.Exec(o.ID, topic.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func newsIsExistById(title string, id uint) (models.News, error) {
	var o models.News
	rows, err := db.Query(
		"SELECT `id` FROM `news` WHERE `title`=? AND `id`<>?",
		title,
		id,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer rows.Close()

	for rows.Next() {
		err := rows.Scan(&o.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}
	}

	err = rows.Err()
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	return o, nil
}

func newsIsExist(title string) (models.News, error) {
	var o models.News

	rows, err := db.Query(
		"SELECT `id` FROM `news` WHERE `title`=?",
		title,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer rows.Close()

	for rows.Next() {
		err := rows.Scan(&o.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}
	}

	err = rows.Err()
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	return o, nil
}

func NewsUpdate(oNew models.News) (models.News, error) {
	news, err := NewsGet(oNew.ID)
	if err != nil {
		return models.News{}, err
	}

	if len(oNew.Title) > 0 && oNew.Title != "" {
		news.Title = oNew.Title
		news.Slug = oNew.Slug
	}

	if len(oNew.Content) > 0 && oNew.Content != "" {
		news.Content = oNew.Content
	}

	if len(oNew.ImageCaption) > 0 && oNew.ImageCaption != "" {
		news.ImageCaption = oNew.ImageCaption
	}

	if len(oNew.Image) > 0 && oNew.Image[0:10] == "data:image" {
		imagePath, err := imageAndUpload(oNew.Image, news.ID, news.Writer.ID)
		libraries.CheckError(err)
		if err != nil {
			return models.News{}, err
		}

		news.Image = imagePath["shared"]
	}

	news.Editor = oNew.Editor

	stmt, err := db.Prepare(
		"UPDATE news" +
			" SET `title`=?, `slug`=?, `content`=?, `image`=?, `image_caption`=?, `editor`=?, `updated_at`=NOW()" +
			" WHERE id=?",
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		news.Title,
		news.Slug,
		news.Content,
		news.Image,
		news.ImageCaption,
		news.Editor.ID,
		news.ID,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, nil
	}

	err = libraries.RedisDeletePrefix("newslist:")
	libraries.CheckError(err)

	return news, nil
}

func NewsPublish(news models.News) (models.News, error) {
	stmt, err := db.Prepare(
		"UPDATE news" +
			" SET `status`='P', `editor`=?, `publish_date`=NOW(), `updated_at`=NOW()" +
			" WHERE id=?",
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		news.Editor.ID,
		news.ID,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, nil
	}

	err = libraries.RedisDeletePrefix("newslist:")
	libraries.CheckError(err)

	return news, nil
}

func NewsDestroy(news models.News) (models.News, error) {
	stmt, err := db.Prepare(
		"UPDATE news" +
			" SET `status`='X', `updated_at`=NOW()" +
			" WHERE id=?",
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		news.ID,
	)
	libraries.CheckError(err)
	if err != nil {
		return models.News{}, nil
	}

	err = libraries.RedisDeletePrefix("newslist:")
	libraries.CheckError(err)

	return news, nil
}

func imageAndUpload(img string, id uint, userId uint) (map[string]string, error) {
	var sizes map[string]string
	sizes = map[string]string{}

	if strings.Index(img, ";base64,") <= 0 {
		err := errors.New("Please supply valid base64 image")
		return map[string]string{}, err
	}

	imageType := img[11:strings.Index(img, ";")]
	baseCode := img[strings.IndexByte(img, ',')+1:]

	h := sha1.New()
	h.Write([]byte(fmt.Sprint(id)))
	bs := h.Sum(nil)
	medianame := hex.EncodeToString(bs[:])
	filename := medianame + fmt.Sprintf("%s.%s", libraries.RandomString(20), imageType)

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(baseCode))
	configure, _, err := image.DecodeConfig(reader)
	if err != nil {
		return map[string]string{}, err
	}

	unbase, err := base64.StdEncoding.DecodeString(baseCode)
	if err != nil {
		return map[string]string{}, err
	}

	if imageType == "gif" {
		sizes["shared"], err = uploadToS3andStoreToMedia(img, "shared/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}

		if configure.Width > 430 {
			r := bytes.NewReader(unbase)
			based, err := resizingImage(r, imageType, 430, medianame)
			if err != nil {
				return map[string]string{}, err
			}

			sizes["thumb"], err = uploadToS3andStoreToMedia(based, "shared_thumb/"+filename, userId)
			if err != nil {
				return map[string]string{}, err
			}
		}

		return sizes, nil
	}

	r := bytes.NewReader(unbase)
	if configure.Width > 1024 {
		based, err := resizingImage(r, imageType, 1024, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["shared"], err = uploadToS3andStoreToMedia(based, "shared/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	} else {
		based, err := resizingImage(r, imageType, 0, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["shared"], err = uploadToS3andStoreToMedia(based, "shared/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	}

	if configure.Width > 800 {
		r := bytes.NewReader(unbase)
		based, err := resizingImage(r, imageType, 800, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["800"], err = uploadToS3andStoreToMedia(based, "shared_800/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	}

	if configure.Width > 480 {
		r := bytes.NewReader(unbase)
		based, err := resizingImage(r, imageType, 480, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["480"], err = uploadToS3andStoreToMedia(based, "shared_480/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	}

	if configure.Width > 430 {
		r := bytes.NewReader(unbase)
		based, err := resizingImage(r, imageType, 430, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["thumb"], err = uploadToS3andStoreToMedia(based, "shared_thumb/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	}

	if configure.Width > 320 {
		r := bytes.NewReader(unbase)
		based, err := resizingImage(r, imageType, 320, medianame)
		if err != nil {
			return map[string]string{}, err
		}

		sizes["320"], err = uploadToS3andStoreToMedia(based, "shared_320/"+filename, userId)
		if err != nil {
			return map[string]string{}, err
		}
	}

	return sizes, nil
}

/*
func getArticleCountByUser(id uint) (map[string]uint, error) {
	var articleSummary map[string]uint
	var p uint
	var j uint

	query, err := db.Query("SELECT COUNT(`id`) FROM articles WHERE `type`='P' AND `writer`=?", id)
	libraries.CheckError(err)
	if err != nil {
		return map[string]uint{}, err
	}

	for query.Next() {
		err := query.Scan(&p)
		libraries.CheckError(err)
		if err != nil {
			return map[string]uint{}, err
		}
	}

	err = query.Err()
	libraries.CheckError(err)
	if err != nil {
		return map[string]uint{}, err
	}

	query.Close()

	rows, err := db.Query("SELECT COUNT(`id`) FROM articles WHERE `type`='J' AND `writer`=?", id)
	libraries.CheckError(err)
	if err != nil {
		return map[string]uint{}, err
	}

	defer rows.Close()

	for rows.Next() {
		err := rows.Scan(&j)
		libraries.CheckError(err)
		if err != nil {
			return map[string]uint{}, err
		}
	}

	err = rows.Err()
	libraries.CheckError(err)
	if err != nil {
		return map[string]uint{}, err
	}

	articleSummary = map[string]uint{"P": p, "J": j}
	return articleSummary, nil
}
*/
func resizingImage(r io.Reader, imgtype string, width uint, medianame string) (string, error) {
	path := "public/uploads/images/"
	filename := libraries.RandomString(20)
	filename += fmt.Sprintf("%s.%s", medianame, imgtype)
	var filepath string

	switch imgtype {
	case "png":
		PNG, err := png.Decode(r)
		if err != nil {
			return "", err
		}

		filepath = path + filename
		m := resize.Resize(width, 0, PNG, resize.Lanczos3)

		os.MkdirAll(path, os.ModePerm)
		f, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
		if err != nil {
			return "", err
		}

		png.Encode(f, m)
		f.Close()
	case "jpeg":
		JPEG, err := jpeg.Decode(r)
		if err != nil {
			return "", err
		}

		filepath = path + filename
		m := resize.Resize(width, 0, JPEG, resize.Lanczos3)

		os.MkdirAll(path, os.ModePerm)
		f, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE, os.ModePerm)
		if err != nil {
			return "", err
		}

		jpeg.Encode(f, m, nil)
		f.Close()
	}

	//open to close image
	o, err := os.Open("./" + filepath)
	if err != nil {
		return "", err
	}

	reader := bufio.NewReader(o)
	content, _ := ioutil.ReadAll(reader)

	encoded := base64.StdEncoding.EncodeToString(content)
	o.Close()

	err = os.Remove("./" + filepath)
	if err != nil {
		return "", err
	}

	based := "data:image/" + imgtype + ";base64," + encoded
	return based, nil
}

func uploadToS3andStoreToMedia(image string, filename string, id uint) (string, error) {
	var imagePath string

	path := "/images/filemanager/" + filename

	err = libraries.AwsUploadS3(image, path)
	libraries.CheckError(err)
	if err != nil {
		return "", err
	}
	imagePath = config.GetString("aws.s3.url") + path

	return imagePath, nil
}
