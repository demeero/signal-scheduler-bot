package inbound

// func TestParserParse(t *testing.T) {
// 	location, err := time.LoadLocation("Europe/Kyiv")
// 	require.NoError(t, err)

// 	parser, err := newParser(location)
// 	require.NoError(t, err)

// 	now := time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC)

// 	tests := []struct {
// 		wantErr error
// 		assert  func(t *testing.T, parsed parsedCommand)
// 		name    string
// 		raw     string
// 	}{
// 		{
// 			name: "help",
// 			raw:  commandHelp,
// 			assert: func(t *testing.T, parsed parsedCommand) {
// 				t.Helper()
// 				_, ok := parsed.(helpCommand)
// 				require.True(t, ok)
// 			},
// 		},
// 		{
// 			name: "schedule by phone",
// 			raw:  "/schedule 2026-06-02 09:30 +380501112233 Добрий ранок!",
// 			assert: func(t *testing.T, parsed parsedCommand) {
// 				t.Helper()
// 				cmd, ok := parsed.(scheduleCommand)
// 				require.True(t, ok)
// 				require.Equal(t, "2026-06-02 09:30", cmd.originalLocalTime)
// 				require.Equal(t, "Europe/Kyiv", cmd.timezone)
// 				require.Equal(t, "phone", cmd.recipientType)
// 				require.Equal(t, "+380501112233", cmd.recipient)
// 				require.Equal(t, "Добрий ранок!", cmd.message)
// 				require.Equal(t, time.Date(2026, time.June, 2, 6, 30, 0, 0, time.UTC), cmd.whenUTC)
// 			},
// 		},
// 		{
// 			name: "schedule by contact name",
// 			raw:  "/schedule tomorrow 09:30 \"Черговий МС\" Добрий ранок!",
// 			assert: func(t *testing.T, parsed parsedCommand) {
// 				t.Helper()
// 				cmd, ok := parsed.(scheduleCommand)
// 				require.True(t, ok)
// 				require.Equal(t, "contact", cmd.recipientType)
// 				require.Equal(t, "Черговий МС", cmd.recipient)
// 				require.Equal(t, "Добрий ранок!", cmd.message)
// 				require.Equal(t, "2026-06-02 09:30", cmd.originalLocalTime)
// 			},
// 		},
// 		{
// 			name: "cancel",
// 			raw:  "/cancel 019d2c5f-03cf-74e5-bc4c-cdca22f72b72",
// 			assert: func(t *testing.T, parsed parsedCommand) {
// 				t.Helper()
// 				cmd, ok := parsed.(cancelCommand)
// 				require.True(t, ok)
// 				require.Equal(t, uuid.MustParse("019d2c5f-03cf-74e5-bc4c-cdca22f72b72"), cmd.id)
// 			},
// 		},
// 		{
// 			name:    "reject schedule in the past",
// 			raw:     "/schedule today 09:00 +380501112233 Запізно",
// 			wantErr: platform.ErrInvalidData,
// 		},
// 		{
// 			name:    "reject unquoted contact",
// 			raw:     "/schedule tomorrow 09:30 Черговий МС Добрий ранок!",
// 			wantErr: platform.ErrInvalidData,
// 		},
// 		{
// 			name:    "reject missing message",
// 			raw:     "/schedule tomorrow 09:30 +380501112233",
// 			wantErr: platform.ErrInvalidData,
// 		},
// 	}

// 	for _, testCase := range tests {
// 		t.Run(testCase.name, func(t *testing.T) {
// 			parsed, err := parser.Parse(testCase.raw, now)
// 			if testCase.wantErr != nil {
// 				require.Error(t, err)
// 				require.ErrorIs(t, err, testCase.wantErr)
// 				return
// 			}

// 			require.NoError(t, err)
// 			testCase.assert(t, parsed)
// 		})
// 	}
// }
