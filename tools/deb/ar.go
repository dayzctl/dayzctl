package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

// arMember is one member of an ar archive: either inline data or a file.
type arMember struct {
	name string
	data []byte
	file string
}

// writeAr writes a classic (System V/GNU) ar archive, which is the container a
// .deb uses.
func writeAr(dest string, members []arMember) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.WriteString("!<arch>\n"); err != nil {
		return err
	}

	mtime := time.Now().Unix()
	for _, m := range members {
		size := int64(len(m.data))
		var src *os.File
		if m.file != "" {
			src, err = os.Open(m.file)
			if err != nil {
				return err
			}
			info, err := src.Stat()
			if err != nil {
				src.Close()
				return err
			}
			size = info.Size()
		}

		// Fixed-width header: name 16, mtime 12, uid 6, gid 6, mode 8, size 10.
		hdr := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n",
			m.name, mtime, 0, 0, "100644", size)
		if len(hdr) != 60 {
			if src != nil {
				src.Close()
			}
			return fmt.Errorf("ar header for %q is %d bytes, want 60", m.name, len(hdr))
		}
		if _, err := out.WriteString(hdr); err != nil {
			if src != nil {
				src.Close()
			}
			return err
		}

		var written int64
		if src != nil {
			written, err = io.Copy(out, src)
			src.Close()
			if err != nil {
				return err
			}
		} else {
			n, err := out.Write(m.data)
			written = int64(n)
			if err != nil {
				return err
			}
		}
		if written != size {
			return fmt.Errorf("member %q: wrote %d bytes, header says %d", m.name, written, size)
		}

		// Members are padded to an even offset.
		if size%2 == 1 {
			if _, err := out.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}
	return out.Close()
}
