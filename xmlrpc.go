package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"http"
	"os"
	"reflect"
	"strconv"
	"time"
	"xml"
)

type Array []interface{}
type Struct map[string]interface{}

var xmlSpecial = map[byte]string{
	'<':  "&lt;",
	'>':  "&gt;",
	'"':  "&quot;",
	'\'': "&apos;",
	'&':  "&amp;",
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		c := s[i]
		if s, ok := xmlSpecial[c]; ok {
			b.WriteString(s)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

type valueNode struct {
	Type string `xml:"attr"`
	Body string `xml:"chardata"`
}

func next(p *xml.Parser) (xml.Name, interface{}, os.Error) {
	se, e := nextStart(p)
	if e != nil {
		return xml.Name{}, nil, e
	}

	var nv interface{}
	var vn valueNode
	switch se.Name.Local {
	case "string":
		if e = p.Unmarshal(&vn, &se); e != nil {
			return xml.Name{}, nil, e
		}
		return xml.Name{}, vn.Body, nil
	case "int", "i4":
		if e = p.Unmarshal(&vn, &se); e != nil {
			return xml.Name{}, nil, e
		}
		i, e := strconv.Atoi64(vn.Body)
		return xml.Name{}, i, e
	case "double":
		if e = p.Unmarshal(&vn, &se); e != nil {
			return xml.Name{}, nil, e
		}
		f, e := strconv.Atof64(vn.Body)
		return xml.Name{}, f, e
	case "dateTime.iso8601":
		if e = p.Unmarshal(&vn, &se); e != nil {
			return xml.Name{}, nil, e
		}
		t, e := time.Parse("20060102T15:04:05", vn.Body)
		if e != nil {
			t, e = time.Parse("2006-01-02T15:04:05-07:00", vn.Body)
			if e != nil {
				t, e = time.Parse("2006-01-02T15:04:05", vn.Body)
			}
		}
		return xml.Name{}, t, e
	case "base64":
		if e = p.Unmarshal(&vn, &se); e != nil {
			return xml.Name{}, nil, e
		}
		if b, e := base64.StdEncoding.DecodeString(vn.Body); e != nil {
			return xml.Name{}, nil, e
		} else {
			return xml.Name{}, b, nil
		}
	case "member":
		nextStart(p)
		return next(p)
	case "value":
		nextStart(p)
		return next(p)
	case "name":
		nextStart(p)
		return next(p)
	case "struct":
		st := Struct{}

		se, e = nextStart(p)
		for e == nil && se.Name.Local == "member" {
			// name
			se, e = nextStart(p)
			if se.Name.Local != "name" {
				return xml.Name{}, nil, os.NewError("invalid response")
			}
			if e != nil {
				break
			}
			if e = p.Unmarshal(&vn, &se); e != nil {
				return xml.Name{}, nil, e
			}
			name := vn.Body
			se, e = nextStart(p)
			if e != nil {
				break
			}

			// value
			_, value, e := next(p)
			if se.Name.Local != "value" {
				return xml.Name{}, nil, os.NewError("invalid response")
			}
			if e != nil {
				break
			}
			se, e = nextStart(p)
			if e != nil {
				break
			}

			st[name] = value
		}
		return xml.Name{}, st, nil
	case "array":
		var ar Array
		nextStart(p) // data
		nextStart(p) // top of value
		for {
			_, value, e := next(p)
			if e != nil {
				break
			}
			ar = append(ar, value)
		}
		return xml.Name{}, ar, nil
	}

	if e = p.Unmarshal(nv, &se); e != nil {
		return xml.Name{}, nil, e
	}
	return se.Name, nv, e
}
func nextStart(p *xml.Parser) (xml.StartElement, os.Error) {
	for {
		t, e := p.Token()
		if e != nil {
			return xml.StartElement{}, e
		}
		switch t := t.(type) {
		case xml.StartElement:
			return t, nil
		}
	}
	panic("unreachable")
}

func to_xml(v interface{}, typ bool) (s string) {
	r := reflect.ValueOf(v)
	t := r.Type()
	k := t.Kind()

	if b, ok := v.([]byte); ok {
		return base64.StdEncoding.EncodeToString(b)
	}

	switch k {
	case reflect.Invalid:
		panic("unsupported type")
	case reflect.Bool:
		return fmt.Sprintf("<boolean>%v</boolean>", v)
	case reflect.Int,
		reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if typ {
			return fmt.Sprintf("<int>%v</int>", v)
		}
		return fmt.Sprintf("%v", v)
	case reflect.Uintptr:
		panic("unsupported type")
	case reflect.Float32, reflect.Float64:
		if typ {
			return fmt.Sprintf("<double>%v</double>", v)
		}
		return fmt.Sprintf("%v", v)
	case reflect.Complex64, reflect.Complex128:
		panic("unsupported type")
	case reflect.Array:
		s = "<array><data>"
		for n := 0; n < r.Len(); n++ {
			s += "<value>"
			s += to_xml(r.Index(n).Interface(), typ)
			s += "</value>"
		}
		s += "</data></array>"
		return s
	case reflect.Chan:
		panic("unsupported type")
	case reflect.Func:
		panic("unsupported type")
	case reflect.Interface:
		return to_xml(r.Elem(), typ)
	case reflect.Map:
		s = "<struct>"
		for _, key := range r.MapKeys() {
			s += "<member>"
			s += "<name>" + xmlEscape(key.Interface().(string)) + "</name>"
			s += "<value>" + to_xml(r.MapIndex(key).Interface(), typ) + "</value>"
			s += "</member>"
		}
		return s
	case reflect.Ptr:
		panic("unsupported type")
	case reflect.Slice:
		panic("unsupported type")
	case reflect.String:
		if typ {
			return fmt.Sprintf("<string>%v</string>", xmlEscape(v.(string)))
		}
		return xmlEscape(v.(string))
	case reflect.Struct:
		s = "<struct>"
		for n := 0; n < r.NumField(); n++ {
			s += "<member>"
			s += "<name>" + t.Field(n).Name + "</name>"
			s += "<value>" + to_xml(r.FieldByIndex([]int{n}).Interface(), true) + "</value>"
			s += "</member>"
		}
		return s
	case reflect.UnsafePointer:
		return to_xml(r.Elem(), typ)
	}
	return
}

func Call(url, name string, args ...interface{}) (v interface{}, e os.Error) {
	s := "<methodCall>"
	s += "<methodName>" + xmlEscape(name) + "</methodName>"
	s += "<params>"
	for _, arg := range args {
		s += "<param><value>"
		s += to_xml(arg, false)
		s += "</value></param>"
	}
	s += "</params></methodCall>"
	bs := bytes.NewBuffer([]byte(s))
	r, e := http.Post(url, "text/xml", bs)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()

	p := xml.NewParser(r.Body)
	se, e := nextStart(p) // methodResponse
	if se.Name.Local != "methodResponse" {
		return nil, os.NewError("invalid response")
	}
	se, e = nextStart(p) // params
	if se.Name.Local != "params" {
		return nil, os.NewError("invalid response")
	}
	se, e = nextStart(p) // param
	if se.Name.Local != "param" {
		return nil, os.NewError("invalid response")
	}
	se, e = nextStart(p) // value
	if se.Name.Local != "value" {
		return nil, os.NewError("invalid response")
	}
	_, v, e = next(p)
	return v, e
}
