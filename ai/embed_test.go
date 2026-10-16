package main

import (
	"fmt"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func TestEmbedReturnsVectorsInTextOrder(t *testing.T) {
	llm := &fakeLLM{}
	h := started(t, withLLM(t, llm, "embed"))
	var res protocol.AIEmbedResult
	if err := h.call(protocol.MethodAIEmbed, protocol.AIEmbedParams{Texts: []string{"a", "b"}}, &res); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(res.Vectors) != "[[0] [1]]" {
		t.Fatalf("vectors %v", res.Vectors)
	}
	if req := llm.lastRequest(); req["model"] != "m" || fmt.Sprint(req["input"]) != "[a b]" {
		t.Fatalf("request %v", req)
	}
}

func TestEmbedNotOfferedIsMethodNotFound(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{}))
	if err := h.call(protocol.MethodAIEmbed, protocol.AIEmbedParams{Texts: []string{"a"}}, nil); rpcCode(err) != protocol.CodeMethodNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestEmbedNeedsTexts(t *testing.T) {
	h := started(t, withLLM(t, &fakeLLM{}, "embed"))
	if err := h.call(protocol.MethodAIEmbed, protocol.AIEmbedParams{}, nil); rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v", err)
	}
}
