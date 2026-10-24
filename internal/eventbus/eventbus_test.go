package eventbus

import (
	"encoding/binary"
	"testing"
)

// 构造一个包含两条插入字符串的 EVENTLOGRECORD 并验证解析。
func TestParseRecords(t *testing.T) {
	msg := "clevocc30^101^2"
	msgUTF16 := make([]byte, 0, (len(msg)+1)*2)
	for _, r := range msg {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(r))
		msgUTF16 = append(msgUTF16, b[:]...)
	}
	msgUTF16 = append(msgUTF16, 0, 0, 0, 0) // 双 NUL 结尾

	const hdr = 56
	recLen := hdr + len(msgUTF16)
	buf := make([]byte, recLen)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(recLen)) // Length
	binary.LittleEndian.PutUint32(buf[4:8], 0)              // Reserved
	binary.LittleEndian.PutUint32(buf[8:12], 1)             // RecordNumber
	binary.LittleEndian.PutUint32(buf[12:16], 0)            // TimeWritten
	binary.LittleEndian.PutUint32(buf[20:24], 1000)         // EventID
	binary.LittleEndian.PutUint16(buf[24:26], eventLogInformation)
	binary.LittleEndian.PutUint16(buf[26:28], 1) // NumStrings
	binary.LittleEndian.PutUint32(buf[36:40], uint32(hdr)) // StringOffset
	copy(buf[hdr:], msgUTF16)

	entries := parseRecords(buf)
	if len(entries) != 1 {
		t.Fatalf("条目数 = %d, want 1", len(entries))
	}
	if entries[0].Message != msg {
		t.Fatalf("消息 = %q, want %q", entries[0].Message, msg)
	}
}
