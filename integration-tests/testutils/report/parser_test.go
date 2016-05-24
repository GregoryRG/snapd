// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package report

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"regexp/syntax"

	"github.com/testing-cabal/subunit-go"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/common"
)

var _ = check.Suite(&ParserReportSuite{})

type StatuserSpy struct {
	calls []subunit.Event
}

func (s *StatuserSpy) Status(event subunit.Event) error {
	s.calls = append(s.calls, event)
	return nil
}

type ParserReportSuite struct {
	subject *SubunitV2ParserReporter
	spy     *StatuserSpy
	output  bytes.Buffer
}

const (
	testID     = "testSuite.testSkippedBySetUp"
	skipReason = "skip reason"
)

var skippedSetUp = []string{
	"START: <autogenerated>:14: testSuite.SetUpSuite\n",
	"PASS: <autogenerated>:34: testSuite.SetUpSuite      0.005s\n",
	fmt.Sprintf("START: /dummy/path:36 %s\n", testID),
	"START: /dummy/path:36 testSuite.SetUpTest\n",
	fmt.Sprintf("SKIP: /dummy/path:36: testSuite.SetUpTest (%s)\n", skipReason),
	fmt.Sprintf("SKIP: /dummy/path:36: %s\n", testID),
}

func (s *ParserReportSuite) SetUpTest(c *check.C) {
	s.spy = &StatuserSpy{}
	s.subject = &SubunitV2ParserReporter{statuser: s.spy}
}

func (s *ParserReportSuite) TestParserSendsNothingWitNotParseableInput(c *check.C) {
	s.subject.Write([]byte("Not parseable"))

	c.Assert(len(s.spy.calls), check.Equals, 0,
		check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
}

var eventTests = []struct {
	gocheckOutput  string
	expectedTestID string
	expectedStatus string
}{{
	"****** Running testSuite.TestExists\n",
	"testSuite.TestExists",
	"exists",
}, {
	"PASS: /tmp/snappy-tests-job/18811/src/github.com/snapcore/snapd/integration-tests/tests/" +
		"apt_test.go:34: testSuite.TestSuccess      0.005s\n",
	"testSuite.TestSuccess",
	"success",
}, {
	"FAIL: /tmp/snappy-tests-job/710/src/github.com/snapcore/snapd/integration-tests/tests/" +
		"installFramework_test.go:85: testSuite.TestFail\n",
	"testSuite.TestFail",
	"fail",
}}

func (s *ParserReportSuite) TestParserReporterSendsEvents(c *check.C) {
	for _, t := range eventTests {
		s.spy.calls = []subunit.Event{}
		s.subject.Write([]byte(t.gocheckOutput))

		c.Check(s.spy.calls, check.HasLen, 1)
		event := s.spy.calls[0]
		c.Check(event.TestID, check.Equals, t.expectedTestID)
		c.Check(event.Status, check.Equals, t.expectedStatus)
	}
}

func (s *ParserReportSuite) TestParserReporterSendsSkipEvent(c *check.C) {
	testID := "testSuite.TestSkip"
	skipReason := "skip reason"
	s.subject.Write([]byte(
		fmt.Sprintf("SKIP: /tmp/snappy-tests-job/21647/src/github.com/snapcore/snapd/"+
			"integration-tests/tests/info_test.go:36: %s (%s)\n", testID, skipReason)))

	c.Check(s.spy.calls, check.HasLen, 1)
	event := s.spy.calls[0]
	c.Check(event.TestID, check.Equals, testID)
	c.Check(event.Status, check.Equals, "skip")
	c.Check(event.MIME, check.Equals, "text/plain;charset=utf8")
	c.Check(event.FileName, check.Equals, "reason")
	c.Check(string(event.FileBytes), check.Equals, skipReason)
}

func (s *ParserReportSuite) TestParserSendsNothingForSetUpAndTearDown(c *check.C) {
	ignoreTests := []string{
		"****** Running testSuite.SetUpTest\n",
		"PASS: /dummy/path:34: testSuite.SetUpTest      0.005s\n",
		"****** Running testSuite.TearDownTest\n",
		"PASS: /dummy/path:34: testSuite.TearDownTest      0.005s\n",
		fmt.Sprintf(
			"SKIP: /dummy/path:36: %s (%s)\n", "testSuite.TestSkip", common.FormatSkipDuringReboot),
		fmt.Sprintf(
			"SKIP: /dummy/path:36: %s (%s)\n", "testSuite.TestSkip", common.FormatSkipAfterReboot),
	}
	for _, gocheckOutput := range ignoreTests {
		s.spy.calls = []subunit.Event{}
		s.subject.Write([]byte(gocheckOutput))

		c.Check(len(s.spy.calls), check.Equals, 0,
			check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
	}
}

func (s *ParserReportSuite) TestParserSendsNothingForTestsAfterReboot(c *check.C) {
	os.Setenv("ADT_REBOOT_MARK", "rebooting")
	defer os.Setenv("ADT_REBOOT_MARK", "")
	ignoreTests := []string{
		"****** Running testSuite.TestSomething\n",
		"PASS: /dummy/path:34: testSuite.TestSomething      0.005s\n",
	}
	for _, gocheckOutput := range ignoreTests {
		s.spy.calls = []subunit.Event{}
		s.subject.Write([]byte(gocheckOutput))

		c.Check(len(s.spy.calls), check.Equals, 0,
			check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
	}
}

func (s *ParserReportSuite) TestParserSendsNothingForTestsDuringReboot(c *check.C) {
	err := ioutil.WriteFile(common.NeedsRebootFile, []byte("rebooting"), 0777)
	c.Assert(err, check.IsNil, check.Commentf("Error writing the reboot file: %v", err))
	defer os.Remove(common.NeedsRebootFile)

	ignoreTests := []string{
		"****** Running testSuite.TestSomething\n",
		"PASS: /dummy/path:34: testSuite.TestSomething      0.005s\n",
	}
	for _, gocheckOutput := range ignoreTests {
		s.spy.calls = []subunit.Event{}
		s.subject.Write([]byte(gocheckOutput))

		c.Check(len(s.spy.calls), check.Equals, 0,
			check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
	}
}

func (s *ParserReportSuite) TestParserSendsNothingForSkippedSetUpTestsAfterReboot(c *check.C) {
	ignoreTests := append([]string{
		fmt.Sprintf(
			"SKIP: /dummy/path:36: %s (%s)\n", "testSuite.TestSkip", common.FormatSkipAfterReboot),
	}, skippedSetUp...)

	s.spy.calls = []subunit.Event{}
	for _, gocheckOutput := range ignoreTests {
		s.subject.Write([]byte(gocheckOutput))

	}
	c.Assert(len(s.spy.calls), check.Equals, 0,
		check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
}

func (s *ParserReportSuite) TestParserSendsNothingForSkippedSetUpTestsDuringReboot(c *check.C) {
	ignoreTests := append([]string{
		fmt.Sprintf(
			"SKIP: /dummy/path:36: %s (%s)\n", "testSuite.TestSkip", common.FormatSkipDuringReboot),
	}, skippedSetUp...)
	s.spy.calls = []subunit.Event{}
	for _, gocheckOutput := range ignoreTests {
		s.subject.Write([]byte(gocheckOutput))
	}
	c.Assert(len(s.spy.calls), check.Equals, 0,
		check.Commentf("Unexpected event sent to subunit: %v", s.spy.calls))
}

func (s *ParserReportSuite) TestParserSendsNothingForSkippedSetUp(c *check.C) {
	testID := "testSuite.SetUpTest"
	skipReason := "skip reason"
	s.subject.Write([]byte(
		fmt.Sprintf("SKIP: /tmp/snappy-tests-job/21647/src/github.com/snapcore/snapd/"+
			"integration-tests/tests/info_test.go:36: %s (%s)\n", testID, skipReason)))

	c.Check(s.spy.calls, check.HasLen, 0)
}

var _ = check.Suite(&ParserHelpersSuite{})

type ParserHelpersSuite struct{}

func (s *ParserHelpersSuite) TestMatchStringPanicsWithBadPatter(c *check.C) {
	c.Assert(func() { matchString("*", "dummy") }, check.Panics,
		&syntax.Error{
			Code: syntax.ErrMissingRepeatArgument,
			Expr: "*"})
}
