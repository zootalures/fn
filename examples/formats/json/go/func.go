package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

type Person struct {
	Name string
}

func (a *Person) UnmarshalJSON(b []byte) error {
	a.Name = string(b)
	return nil
}

type JSONInput struct {
	RequestURL string `json:"request_url"`
	CallID     string `json:"call_id"`
	Method     string `json:"method"`
	Body       Person `json:"body"`
}

func (a *JSONInput) String() string {
	return fmt.Sprintf("request_url=%s\ncall_id=%s\nmethod=%s\n\nbody=%s",
		a.RequestURL, a.CallID, a.Method, a.Body)
}

type JSONOutput struct {
	Body string `json:"body"`
}

func main() {
	// p := &Person{Name: "World"}
	// json.Unmarshal(os.Stdin).Decode(p)
	// mapD := map[string]string{"message": fmt.Sprintf("Hello %s", p.Name)}
	// mapB, _ := json.Marshal(mapD)
	// fmt.Println(string(mapB))
	log.Println("IN func.go")

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		//in := &JSONInput{Body: Person{Name: "World"}}
		in := &JSONInput{}
		if err := dec.Decode(in); err != nil {
			log.Fatalln(err)
			return
		}
		in.Body = Person{Name: "World"}

		log.Println(in)

		mapResult := map[string]string{"message": fmt.Sprintf("Hello %s", in.Body.Name)}
		out := &JSONOutput{}
		b, _ := json.Marshal(mapResult)
		out.Body = string(b)
		if err := enc.Encode(out); err != nil {
			log.Fatalln(err)
		}
	}
}
