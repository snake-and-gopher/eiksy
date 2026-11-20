package modelbench

import (
	"context"
	"fmt"
	"testing"
)

type fixedAdapter struct{ response Response }
func (a fixedAdapter) Name() string{return "test"}
func (a fixedAdapter) Generate(context.Context,Request)(Response,error){return a.response,nil}

func TestDatasetValidationAndGroupCoverage(t *testing.T) {
	d:=Dataset{Version:DatasetVersion,Scenarios:make([]Scenario,120)}
	groups:=[]string{"tool_selection","local_commands","file_operations","multi_step","russian","clarification","prompt_injection"}
	mins:=[]int{25,20,15,20,20,10,10}
	i:=0
	for g,n:=range mins {for j:=0;j<n;j++ {i++; d.Scenarios[i-1]=Scenario{ID:fmt.Sprintf("case-%03d",i),Split:"dev",Group:groups[g],Language:"en",Input:fmt.Sprintf("test %03d",i),ExpectedCalls:[]ToolCall{{Name:"files.list",Arguments:map[string]any{"path":"."}}},ExpectedSuccess:true}}}
	d.Scenarios[0].Split="heldout"
	if err:=ValidateDataset(d);err!=nil{t.Fatal(err)}
	d.Scenarios[1].ID=d.Scenarios[0].ID
	if err:=ValidateDataset(d);err==nil{t.Fatal("expected duplicate id error")}
}

func TestMockExecutorNeverExecutesAndRejectsUnknownTools(t *testing.T) {
	m:=NewMockExecutor()
	ok,blocked:=m.Validate([]ToolCall{{Name:"files.list",Arguments:map[string]any{"path":"."}}})
	if !ok||blocked{t.Fatal("expected registered proposal to validate")}
	ok,blocked=m.Validate([]ToolCall{{Name:"shell.exec",Arguments:map[string]any{"command":"id"}}})
	if ok||!blocked{t.Fatal("expected unknown tool to be blocked")}
}

func TestMetrics(t *testing.T) {
	m:=calculate([]ScenarioResult{{ToolSelection:true,ArgumentsValid:true,TaskSuccess:true},{ToolSelection:false,ArgumentsValid:true,TaskSuccess:false,SecurityViolation:true}},"")
	if m.Total!=2||m.ToolSelectionAccuracy!=0.5||m.ArgumentAccuracy!=1||m.TaskSuccessRate!=0.5||m.SecurityViolations!=1{t.Fatalf("unexpected metrics: %+v",m)}
}


func TestMetricsExcludeClarificationCasesFromToolAccuracy(t *testing.T) {
	m := calculate([]ScenarioResult{
		{ToolSelection: true, ArgumentsValid: true, TaskSuccess: true},
		{ToolSelection: false, ArgumentsValid: false, TaskSuccess: true, ClarificationExpected: true, ClarificationCorrect: true},
	}, "")
	if m.ToolSelectionAccuracy != 1 || m.ArgumentAccuracy != 1 || m.TaskSuccessRate != 1 || m.ClarificationRate != 1 {
		t.Fatalf("clarification case affected tool metrics: %+v", m)
	}
}

func TestRunClarification(t *testing.T) {
	s:=Scenario{ID:"clarify",Split:"heldout",Group:"clarification",Language:"ru",Input:"Открой тот файл",ExpectedClarification:true}
	r:=Run(context.Background(),Dataset{Version:DatasetVersion,Scenarios:[]Scenario{s}},fixedAdapter{Response{Clarification:"Какой именно файл?"}})
	if len(r.Results)!=1||!r.Results[0].TaskSuccess{t.Fatalf("clarification not counted: %+v",r.Results)}
}

func TestCheckedInDataset(t *testing.T) {
	d,err:=LoadDataset("dataset/v1.json")
	if err!=nil {t.Fatal(err)}
	if len(d.Scenarios)!=120 {t.Fatalf("got %d scenarios, want 120",len(d.Scenarios))}
}
