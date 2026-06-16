package main

import "github.com/ehmo/gum/internal/catalog"

// Google Classroom API v1 OAuth scopes.
const (
	scopeClassroomCourses          = "https://www.googleapis.com/auth/classroom.courses"
	scopeClassroomCoursesReadonly  = "https://www.googleapis.com/auth/classroom.courses.readonly"
	scopeClassroomCourseWork       = "https://www.googleapis.com/auth/classroom.coursework.students"
	scopeClassroomCourseWorkRead   = "https://www.googleapis.com/auth/classroom.coursework.students.readonly"
	scopeClassroomRostersReadonly  = "https://www.googleapis.com/auth/classroom.rosters.readonly"
	scopeClassroomAnnounceReadonly = "https://www.googleapis.com/auth/classroom.announcements.readonly"
)

// BuildClassroomOps returns the Google Classroom API v1 surface: courses CRUD,
// courseWork list/get/create, announcements list, students list. typed-rest-sdk,
// byo_oauth. Simple {id}/{courseId} path params.
func BuildClassroomOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeWorkspaceOp(workspaceOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			service: "classroom", riskClass: risk, scopes: scopes,
			httpMethod: method, httpPath: path,
			goPkg: "google.golang.org/api/classroom/v1", goCall: goCall,
		})
	}
	const base = "https://classroom.googleapis.com/v1"
	return []catalog.Op{
		// courses
		op("classroom.courses.list", "classroom.v1.rest.courses.list", "List Classroom Courses",
			"List courses the caller can access (filter by studentId/teacherId/courseStates).",
			catalog.RiskClassRead, []string{scopeClassroomCoursesReadonly}, "GET", base+"/courses", "Courses.List"),
		op("classroom.courses.get", "classroom.v1.rest.courses.get", "Get a Classroom Course",
			"Fetch a course by id.",
			catalog.RiskClassRead, []string{scopeClassroomCoursesReadonly}, "GET", base+"/courses/{id}", "Courses.Get"),
		op("classroom.courses.create", "classroom.v1.rest.courses.create", "Create a Classroom Course",
			"Create a new course (args.body: name, ownerId, section).",
			catalog.RiskClassWrite, []string{scopeClassroomCourses}, "POST", base+"/courses", "Courses.Create"),
		op("classroom.courses.update", "classroom.v1.rest.courses.update", "Update a Classroom Course",
			"Replace a course by id (args.body).",
			catalog.RiskClassWrite, []string{scopeClassroomCourses}, "PUT", base+"/courses/{id}", "Courses.Update"),
		op("classroom.courses.delete", "classroom.v1.rest.courses.delete", "Delete a Classroom Course",
			"Delete a course by id. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, []string{scopeClassroomCourses}, "DELETE", base+"/courses/{id}", "Courses.Delete"),
		// coursework
		op("classroom.courses.courseWork.list", "classroom.v1.rest.courses.courseWork.list", "List Course Work",
			"List the coursework (assignments) in a course.",
			catalog.RiskClassRead, []string{scopeClassroomCourseWorkRead}, "GET", base+"/courses/{courseId}/courseWork", "Courses.CourseWork.List"),
		op("classroom.courses.courseWork.get", "classroom.v1.rest.courses.courseWork.get", "Get Course Work",
			"Fetch a single coursework item by id.",
			catalog.RiskClassRead, []string{scopeClassroomCourseWorkRead}, "GET", base+"/courses/{courseId}/courseWork/{id}", "Courses.CourseWork.Get"),
		op("classroom.courses.courseWork.create", "classroom.v1.rest.courses.courseWork.create", "Create Course Work",
			"Create a coursework item (assignment) in a course (args.body: title, workType, …).",
			catalog.RiskClassWrite, []string{scopeClassroomCourseWork}, "POST", base+"/courses/{courseId}/courseWork", "Courses.CourseWork.Create"),
		// roster + stream
		op("classroom.courses.students.list", "classroom.v1.rest.courses.students.list", "List Course Students",
			"List the students enrolled in a course.",
			catalog.RiskClassRead, []string{scopeClassroomRostersReadonly}, "GET", base+"/courses/{courseId}/students", "Courses.Students.List"),
		op("classroom.courses.announcements.list", "classroom.v1.rest.courses.announcements.list", "List Course Announcements",
			"List the announcements posted to a course stream.",
			catalog.RiskClassRead, []string{scopeClassroomAnnounceReadonly}, "GET", base+"/courses/{courseId}/announcements", "Courses.Announcements.List"),
	}
}
