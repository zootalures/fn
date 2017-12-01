package common

type stack struct {
	inbox chan chan int
	outbox chan chan int
}

func newStack() *stack {
	// stack of inbox items
	list []chan int
}

func (s *stack) run() {
	for {
	// outbox is users requesting from the stack (they send us a channel to send to)
	select {
	case ch := <-outbox:
		send(ch)
	case in := <-inbox:
		s.list = append(s.list, in)
	}
}

func (s *stack) send(ch chan int) {
			for len(s.list) > 0 {
				in := s.list[len(s.list)-1]
				s.list = s.list[:len(s.list)-1]
					select {
					case i, ok := <- in:
						if ok {
						}
					default:
				}
		}
}
