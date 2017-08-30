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

type JSONInput struct {
	RequestURL string `json:"request_url"`
	CallID     string `json:"call_id"`
	Method     string `json:"method"`
	Body       Person `json:"body"`
}

func main() {
	// p := &Person{Name: "World"}
	// json.Unmarshal(os.Stdin).Decode(p)
	// mapD := map[string]string{"message": fmt.Sprintf("Hello %s", p.Name)}
	// mapB, _ := json.Marshal(mapD)
	// fmt.Println(string(mapB))

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		in := &JSONInput{Body: Person{Name: "World"}}
		if err := dec.Decode(in); err != nil {
			log.Fatalln(err)
			return
		}
		mapResult := map[string]string{"message": fmt.Sprintf("Hello %s", in.Body.Name)}
		if err := enc.Encode(mapResult); err != nil {
			log.Fatalln(err)
		}
	}
}
