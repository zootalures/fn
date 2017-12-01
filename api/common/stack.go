package common

type stack struct {
	inbox  chan chan int
	outbox chan chan int

	// stack of inbox items
	list []chan int
}

func newStack() *stack {
	return &stack{
		inbox:  make(chan chan int),
		outbox: make(chan chan int),
	}
}

func (s *stack) run() {
	for {
		// outbox is users requesting from the stack (they send us a channel to send to)
		select {
		case ch := <-s.outbox:
			s.pop(ch)
		case in := <-s.inbox:
			s.list = append(s.list, in)
		}
	}
}

func (s *stack) pop(ch chan int) bool {
	for len(s.list) > 0 {
		in := s.list[len(s.list)-1]
		s.list = s.list[:len(s.list)-1]
		select {
		case i, ok := <-in:
			if ok {
				select {
				case ch <- i:
				default:
					// if they hung up, force them to re-submit
				}
				return true
			}
		default:
		}
	}
	return false
}
