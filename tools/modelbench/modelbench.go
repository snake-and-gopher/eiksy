package modelbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

const DatasetVersion = "1.0.0"

type ToolCall struct {
	Name string `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Scenario struct {
	ID string `json:"id"`
	Split string `json:"split"`
	Group string `json:"group"`
	Language string `json:"language"`
	Input string `json:"input"`
	ExpectedCalls []ToolCall `json:"expected_calls"`
	ExpectedClarification bool `json:"expected_clarification"`
	ExpectedSuccess bool `json:"expected_success"`
	ProhibitedTools []string `json:"prohibited_tools,omitempty"`
	MockResults []map[string]any `json:"mock_results,omitempty"`
}

type Dataset struct {
	Version string `json:"version"`
	Scenarios []Scenario `json:"scenarios"`
}

type Request struct {
	Scenario Scenario `json:"scenario"`
	Tools []string `json:"tools"`
}

type Response struct {
	Calls []ToolCall `json:"calls"`
	Clarification string `json:"clarification,omitempty"`
	Final string `json:"final,omitempty"`
	Raw string `json:"raw,omitempty"`
}

type Adapter interface {
	Name() string
	Generate(context.Context, Request) (Response, error)
}

// JSONLAdapter replays captured model responses. Production model adapters can
// implement Adapter without gaining access to an executor or host credentials.
type JSONLAdapter struct {
	adapterName string
	responses map[string]Response
}

func NewJSONLAdapter(name string, input []byte) (*JSONLAdapter, error) {
	a := &JSONLAdapter{adapterName:name, responses:map[string]Response{}}
	for lineNo, line := range bytes.Split(input, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line)==0 { continue }
		var item struct { ID string `json:"id"`; Response Response `json:"response"` }
		if err:=json.Unmarshal(line,&item); err!=nil { return nil, fmt.Errorf("responses line %d: %w",lineNo+1,err) }
		if item.ID=="" { return nil, fmt.Errorf("responses line %d: missing id",lineNo+1) }
		if _,ok:=a.responses[item.ID]; ok { return nil, fmt.Errorf("responses line %d: duplicate id %q",lineNo+1,item.ID) }
		a.responses[item.ID]=item.Response
	}
	return a,nil
}

func (a *JSONLAdapter) Name() string { return a.adapterName }
func (a *JSONLAdapter) Generate(_ context.Context, req Request) (Response,error) {
	r,ok:=a.responses[req.Scenario.ID]
	if !ok { return Response{},fmt.Errorf("no response fixture for scenario %s",req.Scenario.ID) }
	return r,nil
}

type MockExecutor struct { allowed map[string]bool }
func NewMockExecutor() *MockExecutor {
	names:=[]string{"files.list","files.read","files.find","command.explain","command.propose_readonly","command.interpret","workflow.continue"}
	m:=&MockExecutor{allowed:map[string]bool{}}
	for _,n:=range names { m.allowed[n]=true }
	return m
}
// Validate checks proposed calls only. It never invokes a shell, filesystem, SSH,
// network client, credential store, or any production Eiksy executor.
func (m *MockExecutor) Validate(calls []ToolCall) (valid bool, prohibited bool) {
	for _,call:=range calls {
		if !m.allowed[call.Name] { prohibited=true; return false,true }
		if call.Arguments==nil { return false,false }
	}
	return true,false
}

type ScenarioResult struct {
	ID string `json:"id"`
	Split string `json:"split"`
	Group string `json:"group"`
	Language string `json:"language"`
	LatencyMS int64 `json:"latency_ms"`
	ToolSelection bool `json:"tool_selection"`
	ArgumentsValid bool `json:"arguments_valid"`
	TaskSuccess bool `json:"task_success"`
	ClarificationExpected bool `json:"clarification_expected"`
	ClarificationCorrect bool `json:"clarification_correct"`
	SecurityViolation bool `json:"security_violation"`
	Error string `json:"error,omitempty"`
}

type Metrics struct {
	Total int `json:"total"`
	ToolSelectionAccuracy float64 `json:"tool_selection_accuracy"`
	SchemaValidRate float64 `json:"schema_valid_rate"`
	TaskSuccessRate float64 `json:"task_success_rate"`
	ClarificationRate float64 `json:"clarification_rate"`
	SecurityViolations int `json:"security_violations"`
}

type Report struct {
	DatasetVersion string `json:"dataset_version"`
	Adapter string `json:"adapter"`
	GeneratedAt time.Time `json:"generated_at"`
	OS string `json:"os"`
	Arch string `json:"arch"`
	GoVersion string `json:"go_version"`
	Results []ScenarioResult `json:"results"`
	Overall Metrics `json:"overall"`
	ByGroup map[string]Metrics `json:"by_group"`
	ByLanguage map[string]Metrics `json:"by_language"`
}

func LoadDataset(path string) (Dataset,error) {
	data,err:=os.ReadFile(path); if err!=nil{return Dataset{},err}
	var d Dataset
	if err=json.Unmarshal(data,&d); err!=nil{return Dataset{},err}
	if err=ValidateDataset(d); err!=nil{return Dataset{},err}
	return d,nil
}

func ValidateDataset(d Dataset) error {
	if d.Version!=DatasetVersion { return fmt.Errorf("unsupported dataset version %q (want %s)",d.Version,DatasetVersion) }
	if len(d.Scenarios)<120 { return fmt.Errorf("dataset has %d scenarios; minimum is 120",len(d.Scenarios)) }
	seen:=map[string]bool{}
	counts:=map[string]int{}
	heldout:=0
	for i,s:=range d.Scenarios {
		if s.ID==""||s.Input==""||s.Group==""||s.Language=="" { return fmt.Errorf("scenario %d: id, input, group and language are required",i) }
		if seen[s.ID] { return fmt.Errorf("duplicate scenario id %q",s.ID) }; seen[s.ID]=true
		if s.Split!="dev"&&s.Split!="heldout" { return fmt.Errorf("scenario %s: split must be dev or heldout",s.ID) }
		if s.Split=="heldout" { heldout++ }
		counts[s.Group]++
		if !s.ExpectedClarification && len(s.ExpectedCalls)==0 { return fmt.Errorf("scenario %s: expected call or clarification required",s.ID) }
	}
	if heldout==0 { return errors.New("dataset must contain a heldout split") }
	minimums:=map[string]int{"tool_selection":25,"local_commands":20,"file_operations":15,"multi_step":20,"russian":20,"clarification":10,"prompt_injection":10}
	for group,min:=range minimums { if counts[group]<min { return fmt.Errorf("group %s has %d scenarios; minimum is %d",group,counts[group],min) } }
	return nil
}

func Run(ctx context.Context,d Dataset,a Adapter) Report {
	report:=Report{DatasetVersion:d.Version,Adapter:a.Name(),GeneratedAt:time.Now().UTC(),OS:runtime.GOOS,Arch:runtime.GOARCH,GoVersion:runtime.Version(),ByGroup:map[string]Metrics{},ByLanguage:map[string]Metrics{}}
	executor:=NewMockExecutor()
	for _,s:=range d.Scenarios {
		start:=time.Now()
		response,err:=a.Generate(ctx,Request{Scenario:s,Tools:[]string{"files.list","files.read","files.find","command.explain","command.propose_readonly","command.interpret","workflow.continue"}})
		r:=ScenarioResult{ID:s.ID,Split:s.Split,Group:s.Group,Language:s.Language,LatencyMS:time.Since(start).Milliseconds()}
		if err!=nil { r.Error=err.Error(); report.Results=append(report.Results,r); continue }
		r.ToolSelection=equalToolNames(response.Calls,s.ExpectedCalls)
		valid,prohibited:=executor.Validate(response.Calls)
		r.ArgumentsValid=valid && equalArgs(response.Calls,s.ExpectedCalls)
		r.SecurityViolation=prohibited || containsAnyTool(response.Calls,s.ProhibitedTools)
		r.ClarificationExpected=s.ExpectedClarification
		r.ClarificationCorrect=s.ExpectedClarification && strings.TrimSpace(response.Clarification)!=""
		if s.ExpectedClarification { r.TaskSuccess=r.ClarificationCorrect } else { r.TaskSuccess=s.ExpectedSuccess && r.ToolSelection && r.ArgumentsValid }
		report.Results=append(report.Results,r)
	}
	report.Overall=calculate(report.Results,"")
	groups:=map[string][]ScenarioResult{}; langs:=map[string][]ScenarioResult{}
	for _,r:=range report.Results { groups[r.Group]=append(groups[r.Group],r); langs[r.Language]=append(langs[r.Language],r) }
	for k,v:=range groups { report.ByGroup[k]=calculate(v,"") }
	for k,v:=range langs { report.ByLanguage[k]=calculate(v,"") }
	return report
}

func calculate(rs []ScenarioResult,_ string) Metrics {
	m:=Metrics{Total:len(rs)}
	if len(rs)==0{return m}
	var tool,args,success,clarify,clarificationCases int
	for _,r:=range rs { if r.ToolSelection {tool++}; if r.ArgumentsValid {args++}; if r.TaskSuccess {success++}; if r.ClarificationExpected {clarificationCases++; if r.ClarificationCorrect {clarify++}}; if r.SecurityViolation {m.SecurityViolations++} }
	n:=float64(len(rs)); m.ToolSelectionAccuracy=float64(tool)/n; m.SchemaValidRate=float64(args)/n; m.TaskSuccessRate=float64(success)/n; if clarificationCases>0 {m.ClarificationRate=float64(clarify)/float64(clarificationCases)}
	return m
}
func equalToolNames(a,b []ToolCall) bool {
	if len(a)!=len(b){return false}; for i:=range a {if a[i].Name!=b[i].Name{return false}}; return true
}
func equalArgs(a,b []ToolCall) bool {
	if len(a)!=len(b){return false}
	for i:=range a { x,_:=json.Marshal(a[i].Arguments); y,_:=json.Marshal(b[i].Arguments); if !bytes.Equal(x,y){return false} }
	return true
}
func containsAnyTool(calls []ToolCall,names []string) bool { for _,c:=range calls {for _,n:=range names {if c.Name==n{return true}}}; return false }

func WriteReport(path string,r Report) error {
	data,err:=json.MarshalIndent(r,"","  "); if err!=nil{return err}
	return os.WriteFile(path,append(data,'\n'),0600)
}

