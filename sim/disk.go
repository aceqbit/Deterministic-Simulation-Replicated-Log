package sim

// SectorSize is the granularity at which a crash can tear a write.
const SectorSize = 512

type diskWrite struct {
	id   uint64
	off  int
	data []byte
}

// disk is an in-memory block device holding one append-only log file. Writes
// sit in a volatile buffer until a sync covering them completes.
type disk struct {
	durable []byte
	pend    []diskWrite
	size    int    // logical length including unsynced writes
	nextID  uint64 // id of the next write
}

// append adds p at the end of the logical file and returns the write id.
func (d *disk) append(p []byte) uint64 {
	d.nextID++
	d.pend = append(d.pend, diskWrite{id: d.nextID, off: d.size, data: append([]byte(nil), p...)})
	d.size += len(p)
	return d.nextID
}

func (d *disk) place(off int, data []byte) {
	if end := off + len(data); end > len(d.durable) {
		d.durable = append(d.durable, make([]byte, end-len(d.durable))...)
	}
	copy(d.durable[off:], data)
}

// syncUpTo makes every write with id <= upto durable.
func (d *disk) syncUpTo(upto uint64) {
	k := 0
	for k < len(d.pend) && d.pend[k].id <= upto {
		d.place(d.pend[k].off, d.pend[k].data)
		k++
	}
	d.pend = append(d.pend[:0], d.pend[k:]...)
}

// crash resolves every unsynced write: kept, dropped (when lost) or torn at
// sector granularity (when torn), each chosen through src. It returns the
// number of dropped and torn writes.
func (d *disk) crash(src *ChoiceSource, lost, torn bool) (dropped, tornN int) {
	for _, w := range d.pend {
		choice := uint64(0) // 0 keep, 1 drop, 2 tear
		switch {
		case lost && torn:
			choice = src.Draw(3)
		case lost:
			choice = src.Draw(2)
		case torn:
			choice = src.Draw(2)
			if choice == 1 {
				choice = 2
			}
		}
		switch choice {
		case 0:
			d.place(w.off, w.data)
		case 1:
			dropped++
		case 2:
			tornN++
			d.tear(w, src)
		}
	}
	d.pend = d.pend[:0]
	d.size = len(d.durable)
	return dropped, tornN
}

// tear keeps each sector of w independently; at least one sector is lost.
func (d *disk) tear(w diskWrite, src *ChoiceSource) {
	first := w.off / SectorSize
	last := (w.off + len(w.data) - 1) / SectorSize
	n := last - first + 1
	lose := int(src.Draw(uint64(n)))
	for s := first; s <= last; s++ {
		if s-first == lose || (s-first != lose && src.Draw(2) == 0) {
			continue
		}
		lo := max(s*SectorSize, w.off)
		hi := min((s+1)*SectorSize, w.off+len(w.data))
		d.place(lo, w.data[lo-w.off:hi-w.off])
	}
}

// truncate shrinks the durable file to n bytes (recovery truncates the torn tail).
func (d *disk) truncate(n int) {
	d.durable = d.durable[:n]
	d.pend = d.pend[:0]
	d.size = n
}
