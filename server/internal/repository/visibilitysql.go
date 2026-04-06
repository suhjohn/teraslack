package repository

import "fmt"

func ConversationVisibilityPredicate(conversationAlias string, userExpr string) string {
	return fmt.Sprintf(`(
		(%[1]s.workspace_id is null and %[1]s.access_policy = 'authenticated')
		or (
			%[1]s.workspace_id is null
			and %[1]s.access_policy = 'members'
			and exists (
				select 1
				from conversation_participants cp
				where cp.conversation_id = %[1]s.id
				  and cp.user_id = %[2]s
			)
		)
		or (
			%[1]s.workspace_id is not null
			and exists (
				select 1
				from workspace_memberships wm
				where wm.workspace_id = %[1]s.workspace_id
				  and wm.user_id = %[2]s
				  and wm.status = 'active'
			)
			and (
				%[1]s.access_policy = 'workspace'
				or exists (
					select 1
					from conversation_participants cp
					where cp.conversation_id = %[1]s.id
					  and cp.user_id = %[2]s
				)
			)
		)
	)`, conversationAlias, userExpr)
}

func ExternalEventVisibilityPredicate(eventAlias string, userExpr string) string {
	return fmt.Sprintf(`(
		exists (
			select 1
			from user_event_feed uef
			where uef.external_event_id = %[1]s.id
			  and uef.user_id = %[2]s
		)
		or exists (
			select 1
			from workspace_event_feed wef
			join workspace_memberships wm
			  on wm.workspace_id = wef.workspace_id
			 and wm.user_id = %[2]s
			 and wm.status = 'active'
			where wef.external_event_id = %[1]s.id
		)
		or exists (
			select 1
			from conversation_event_feed cef
			join conversations c on c.id = cef.conversation_id
			where cef.external_event_id = %[1]s.id
			  and %[3]s
		)
	)`, eventAlias, userExpr, ConversationVisibilityPredicate("c", userExpr))
}
