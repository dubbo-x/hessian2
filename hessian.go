// Copyright 2016-2019 Alex Stocks
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hessian

import (
	"bufio"
	"encoding/binary"
	"reflect"
	"time"
)

import (
	perrors "github.com/pkg/errors"
)

// enum part
const (
	PackageError              = PackageType(0x01)
	PackageRequest            = PackageType(0x02)
	PackageResponse           = PackageType(0x04)
	PackageHeartbeat          = PackageType(0x08)
	PackageRequest_TwoWay     = PackageType(0x10)
	PackageResponse_Exception = PackageType(0x20)
)

// PackageType ...
type PackageType int

type DubboHeader struct {
	MagicNumber    uint16
	HType          uint8
	ResponseStatus uint8
	ID             uint64
	BodyLen        uint32
}

func (header *DubboHeader) GetSerialID() uint8 {
	return header.HType & SERIAL_MASK
}

func (header *DubboHeader) SetSerialID(serialID uint8) {
	header.HType |= serialID
}

// Service defines service instance
type Service struct {
	Path      string
	Interface string
	Version   string
	Target    string // Service Name
	Method    string
	Timeout   time.Duration // request timeout
}

// HessianCodec defines hessian codec
type HessianCodec struct {
	pkgType PackageType
	reader  *bufio.Reader
	bodyLen int
}

// NewHessianCodec generate a new hessian codec instance
func NewHessianCodec(reader *bufio.Reader) *HessianCodec {
	return &HessianCodec{
		reader: reader,
	}
}

func NewHessianCodecWithType(reader *bufio.Reader, packageType PackageType) *HessianCodec {
	return &HessianCodec{
		pkgType: packageType,
		reader:  reader,
	}
}

func (h *HessianCodec) Write(service Service, header DubboHeader, body interface{}) ([]byte, error) {
	switch h.pkgType {
	case PackageHeartbeat:
		if header.ResponseStatus == Zero {
			return packRequest(h.pkgType, service, header, body)
		}
		return packResponse(h.pkgType, header, map[string]string{}, body)
	case PackageRequest, PackageRequest_TwoWay:
		return packRequest(h.pkgType, service, header, body)

	case PackageResponse:
		return packResponse(h.pkgType, header, map[string]string{}, body)

	default:
		return nil, perrors.Errorf("Unrecognised message type: %v", h.pkgType)
	}

	// unreachable return nil, nil
}

// ReadHeader uses hessian codec to read dubbo header
func (h *HessianCodec) ReadHeader(header *DubboHeader) error {
	err := binary.Read(h.reader, binary.BigEndian, header)
	if err != nil {
		return perrors.WithStack(err)
	}

	flag := header.HType & FLAG_EVENT
	if flag != Zero {
		h.pkgType |= PackageHeartbeat
	}
	flag = header.HType & FLAG_REQUEST
	if flag != Zero {
		h.pkgType |= PackageRequest
		flag = header.HType & FLAG_TWOWAY
		if flag != Zero {
			h.pkgType |= PackageRequest_TwoWay
		}
	} else {
		h.pkgType |= PackageResponse
		if header.ResponseStatus != Response_OK {
			h.pkgType |= PackageResponse_Exception
		}
	}

	h.bodyLen = int(header.BodyLen)

	return perrors.WithStack(err)

}

// ReadBody uses hessian codec to read response body
func (h *HessianCodec) ReadBody(rspObj interface{}) error {

	if h.reader.Buffered() < h.bodyLen {
		return ErrBodyNotEnough
	}
	buf, err := h.reader.Peek(h.bodyLen)
	if err != nil {
		return perrors.WithStack(err)
	}
	_, err = h.reader.Discard(h.bodyLen)
	if err != nil { // this is impossible
		return perrors.WithStack(err)
	}

	switch h.pkgType & 0x2f {
	case PackageResponse | PackageHeartbeat | PackageResponse_Exception, PackageResponse | PackageResponse_Exception:
		rsp, ok := rspObj.(*Response)
		if !ok {
			return perrors.Errorf("@rspObj is not *Response, it is %s", reflect.TypeOf(rspObj).String())
		}
		rsp.Exception = ErrJavaException
		if h.bodyLen > 1 {
			rsp.Exception = perrors.Errorf("java exception:%s", string(buf[1:h.bodyLen-1]))
		}
		return nil
	case PackageRequest | PackageHeartbeat, PackageResponse | PackageHeartbeat:
	case PackageRequest:
		if rspObj != nil {
			if err = unpackRequestBody(buf, rspObj); err != nil {
				return perrors.WithStack(err)
			}
		}
	case PackageResponse:
		if rspObj != nil {
			rsp, ok := rspObj.(*Response)
			if !ok {
				rsp = &Response{RspObj: rspObj}
			}
			if err = unpackResponseBody(buf, rsp); err != nil {
				return perrors.WithStack(err)
			}
		}
	}

	return nil
}
