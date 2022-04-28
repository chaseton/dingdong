package session

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/imroc/req/v3"

	"dingdong/internal/app/config"
	"dingdong/internal/app/dto/address"
	"dingdong/internal/app/pkg/errs"
	"dingdong/internal/app/pkg/errs/code"
	"dingdong/pkg/js"
	"dingdong/pkg/json"
	"dingdong/pkg/textual"
)

var (
	once sync.Once
	s    *session
)

type session struct {
	UserID  string
	Client  *req.Client
	Address address.Item
}

func Initialize() {
	once.Do(func() {
		// client := req.DevMode().EnableForceHTTP1()
		// client := req.C().EnableForceHTTP1()
		client := req.C().EnableForceHTTP1().
			SetCommonRetryCondition(retryCondition).
			SetCommonRetryInterval(retryInterval).
			SetCommonRetryHook(retryHook).
			SetCommonRetryBackoffInterval(1*time.Millisecond, 10*time.Millisecond)

		s = &session{
			Client: client,
		}

		setUserID()
		chooseAddr()
	})
}

func InitializeMock() {
	once.Do(func() {
		client := req.DevMode().EnableForceHTTP1()
		// client := req.C().EnableForceHTTP1()

		s = &session{
			Client: client,
		}

		conf := config.Get()
		s.UserID = conf.Mock["ddmc-uid"]
		s.Address = address.Item{
			Id:         conf.Mock["address_id"],
			CityNumber: conf.Mock["ddmc-city-number"],
			StationId:  conf.Mock["ddmc-station-id"],
		}
		longitude, _ := strconv.ParseFloat(conf.Mock["ddmc-longitude"], 64)
		latitude, _ := strconv.ParseFloat(conf.Mock["ddmc-latitude"], 64)
		s.Address.Location.Location = []float64{longitude, latitude}
	})
}

func retryCondition(resp *req.Response, err error) bool {
	if err != nil || resp.StatusCode != http.StatusOK {
		return true
	}
	body, err := resp.ToBytes()
	if err != nil {
		return true
	}
	success := json.Get(body, "success").ToBool()
	return !success
}

func retryInterval(resp *req.Response, attempt int) time.Duration {
	duration := 150 + rand.Intn(50)
	return time.Duration(duration) * time.Millisecond
}

func retryHook(resp *req.Response, err error) {
	if err != nil {
		log.Println("Request error =>", err.Error())
	}
	r := resp.Request.RawRequest
	log.Println("Retry request =>", r.Method, r.URL)
}

func Client() *req.Client {
	return s.Client
}

func Address() address.Item {
	return s.Address
}

func setUserID() {
	user, err := GetUser()
	if err != nil {
		panic(err)
	}
	s.UserID = user.ID
}

func chooseAddr() {
	addrList, err := GetAddress()
	if err != nil {
		panic(err)
	}
	if len(addrList) == 1 {
		s.Address = addrList[0]
		log.Println(json.MustEncodePrettyString(s.Address))
		log.Printf("默认收货地址: %s %s %s", s.Address.Location.Address, s.Address.Location.Name, s.Address.AddrDetail)
		return
	}

	options := make([]string, 0, len(addrList))
	for _, v := range addrList {
		options = append(options, fmt.Sprintf("%s %s %s", v.Location.Address, v.Location.Name, v.AddrDetail))
	}

	var addr string
	sv := &survey.Select{
		Message: "请选择收货地址",
		Options: options,
	}
	if err := survey.AskOne(sv, &addr); err != nil {
		panic(errs.Wrap(code.SelectAddressFailed, err))
	}

	index := textual.IndexOf(addr, options)
	s.Address = addrList[index]
	log.Printf("已选择收货地址: %s %s %s", s.Address.Location.Address, s.Address.Location.Name, s.Address.AddrDetail)
	return
}

func GetHeaders() map[string]string {
	headers := map[string]string{
		// "accept-encoding" : "gzip, deflate, br", // 压缩有乱码
		// "Connection" : "keep-alive",
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh-Hans;q=0.9",
		"Content-Type":       "application/x-www-form-urlencoded",
		"ddmc-city-number":   s.Address.CityNumber,
		"ddmc-station-id":    s.Address.StationId,
		"ddmc-longitude":     strconv.FormatFloat(s.Address.Location.Location[0], 'f', -1, 64),
		"ddmc-latitude":      strconv.FormatFloat(s.Address.Location.Location[1], 'f', -1, 64),
		"ddmc-uid":           s.UserID,
		"ddmc-time":          strconv.Itoa(int(time.Now().Unix())),
		"ddmc-app-client-id": "3",
		"ddmc-api-version":   "9.50.2",
		"ddmc-build-version": "2.85.3",
		"ddmc-channel":       "undefined",
		"ddmc-device-id":     "",
		"ddmc-ip":            "",
		"ddmc-os-version":    "undefined",
	}
	h := config.Get().Headers
	log.Printf("custom headers: %#v", h)
	for k, v := range h {
		headers[k] = v
	}
	return headers
}

func GetParams(headers map[string]string) map[string]string {
	params := map[string]string{
		"uid":           headers["ddmc-uid"],
		"longitude":     headers["ddmc-longitude"],
		"latitude":      headers["ddmc-latitude"],
		"station_id":    headers["ddmc-station-id"],
		"city_number":   headers["ddmc-city-number"],
		"api_version":   headers["ddmc-api-version"],
		"app_version":   headers["ddmc-build-version"],
		"applet_source": "",
		"app_client_id": "3",
		"h5_source":     "",
		"sharer_uid":    "",
		"s_id":          strings.TrimLeft(strings.Split(headers["Cookie"], ";")[0], "DDXQSESSID="),
		"openid":        "",
		"time":          headers["ddmc-time"],
	}
	p := config.Get().Params
	for k, v := range p {
		params[k] = v
	}
	return params
}

func Sign(params map[string]string) (map[string]string, error) {
	res, err := js.Call("js/sign.js", "sign", json.MustEncodeToString(params))
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	json.MustDecodeFromString(res.String(), &m)
	params["nars"] = m["nars"]
	params["sesi"] = m["sesi"]
	return params, nil
}
